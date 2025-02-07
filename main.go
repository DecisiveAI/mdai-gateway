package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"sync"
	"time"

	"github.com/cenkalti/backoff/v4"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	mdaiv1 "github.com/DecisiveAI/mdai-operator/api/v1"

	"github.com/decisiveai/event-handler-webservice/types"
	"github.com/valkey-io/valkey-go"
)

const (
	valkeyEndpointEnvVarKey = "VALKEY_ENDPOINT"
	valkeyPasswordEnvVarKey = "VALKEY_PASSWORD"

	firingStatus   = "firing"
	resolvedStatus = "resolved"

	actionContextAnnotationsKey  = "action_context"
	relevantLabelsAnnotationsKey = "relevant_labels"
	HubName                      = "hub_name"

	AddElement    = "mdai/add_element"
	RemoveElement = "mdai/remove_element"
	ReplaceValue  = "mdai/replace_element"

	VariableKeyPrefix = "variable/"

	StreamName    = "events"
	ConsumerGroup = "consumer-group-1"
	ConsumerName  = "consumer-1"

	SuccessResponse = `{"success": "variable(s) updated"}`
)

var (
	processMutex sync.Mutex
	logger       *zap.Logger
)

func main() {
	var valkeyClient valkey.Client
	ctx := context.Background()
	setupLogger(zap.DebugLevel)
	operation := func() error {
		var err error
		valkeyClient, err = valkey.NewClient(valkey.ClientOption{
			InitAddress: []string{getEnvVariableWithDefault(valkeyEndpointEnvVarKey, "mdai-valkey-primary.mdai.svc.cluster.local:6379")},
			Password:    getEnvVariableWithDefault(valkeyPasswordEnvVarKey, ""),
		})
		if err != nil {
			logger.Info("failed to initialize valkey client. retrying...")
			return err
		}
		return nil
	}

	backoffConfig := backoff.NewExponentialBackOff()
	backoffConfig.InitialInterval = 5 * time.Second
	backoffConfig.MaxElapsedTime = 3 * time.Minute

	if err := backoff.Retry(operation, backoffConfig); err != nil {
		logger.Fatal("failed to get valkey client", zap.Error(err))
	}

	_ = valkeyClient.Do(ctx,
		valkeyClient.B().XgroupCreate().
			Key(StreamName).
			Group(ConsumerGroup).
			Id("0").
			Mkstream().
			Build(),
	)

	go processAlertsQueue(ctx, valkeyClient)

	http.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))
	server := &http.Server{Addr: ":8081"}
	go func() {
		logger.Info("Starting server", zap.String("address", ":8081"))
		logger.Fatal("failed to start server", zap.Error(server.ListenAndServe()))
	}()
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		logger.Fatal("failed to shutdown server", zap.Error(err))
	}
}

func getEnvVariableWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func handleAlertsPost(ctx context.Context, valkeyClient valkey.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		processMutex.Lock()
		defer processMutex.Unlock()

		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Info("failed to read body", zap.Error(err))
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		logger.Info("Received payload", zap.ByteString("payload", body))

		var payload types.AlertManagerPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			logger.Info("Invalid JSON", zap.ByteString("body", body), zap.Error(err))
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// processing the alerts in chronological order
		sort.Slice(payload.Alerts, func(i, j int) bool {
			return payload.Alerts[i].StartsAt.Before(payload.Alerts[j].StartsAt)
		})

		var cmds valkey.Commands
		for _, alert := range payload.Alerts {
			hubName := alert.Annotations[HubName]
			if hubName == "" {
				logger.Info("Skipping alert because no hub_name found in alert annotations", zap.Any("payload", payload), zap.Any("alert", alert))
				continue
			}

			actionContextJSON := alert.Annotations[actionContextAnnotationsKey]
			if actionContextJSON == "" {
				logger.Info("Skipping alert, missing action_context", zap.Any("alert", alert))
				continue
			}
			relevantLabelsJSON := alert.Annotations[relevantLabelsAnnotationsKey]
			if relevantLabelsJSON == "" {
				logger.Info("Skipping alert, missing relevant_labels", zap.Any("alert", alert))
				continue
			}

			var actionContext mdaiv1.PrometheusAlertEvaluationStatus
			if err := json.Unmarshal([]byte(actionContextJSON), &actionContext); err != nil {
				logger.Info("Could not unmarshal action_context", zap.Any("alert", alert), zap.Error(err))
				continue
			}
			relevantLabels := make([]string, 0)
			if err := json.Unmarshal([]byte(relevantLabelsJSON), &relevantLabels); err != nil {
				logger.Info("Could not unmarshal relevant_labels", zap.Any("alert", alert), zap.Error(err))
				continue
			}

			logger.Info("Processing alert", zap.Any("alert", alert))

			var variableUpdate *mdaiv1.VariableUpdate
			switch alert.Status {
			case firingStatus:
				if actionContext.Firing == nil || actionContext.Firing.VariableUpdate == nil {
					logger.Error("No firing context found for alert", zap.Any("alert", alert))
					continue
				}
				variableUpdate = actionContext.Firing.VariableUpdate
			case resolvedStatus:
				if actionContext.Resolved == nil || actionContext.Resolved.VariableUpdate == nil {
					logger.Error("No resolved context found for alert", zap.Any("alert", alert))
					continue
				}
				variableUpdate = actionContext.Resolved.VariableUpdate
			default:
				logger.Error("Invalid alert status: %s, payload: %v", zap.Any("alert", alert))
				continue
			}

			valkeyKey := VariableKeyPrefix + hubName + "/" + variableUpdate.VariableRef

			mdaiHubEvent := types.MdaiHubEvent{
				Type:      "variable_update_queued",
				Operation: variableUpdate.Operation,
				Variable:  valkeyKey,
			}

			switch variableUpdate.Operation {
			case AddElement, RemoveElement:
				for _, element := range relevantLabels {
					cmds = append(cmds, valkeyClient.B().Xadd().Key(StreamName).Id("*").FieldValue().FieldValue("action", variableUpdate.Operation).FieldValue("key", valkeyKey).FieldValue("value", alert.Labels[element]).Build())

					mdaiHubEvent.Value = alert.Labels[element]
					logger.Info("Queueing "+variableUpdate.Operation,
						zap.String("variable", valkeyKey),
						zap.Any("mdaiHubEvent", mdaiHubEvent),
					)
				}
			case ReplaceValue:
				if len(relevantLabels) > 1 {
					logger.Info("Multiple relevantLabels found for replace action",
						zap.String("selected_label", relevantLabels[0]),
						zap.Any("all_labels", relevantLabels),
					)
				}
				mdaiHubEvent.Value = alert.Labels[relevantLabels[0]]
				logger.Info("Queueing "+variableUpdate.Operation,
					zap.String("variable", valkeyKey),
					zap.Any("mdaiHubEvent", mdaiHubEvent),
				)

				cmds = append(cmds, valkeyClient.B().Xadd().Key(StreamName).Id("*").FieldValue().FieldValue("action", variableUpdate.Operation).FieldValue("key", valkeyKey).FieldValue("value", alert.Labels[relevantLabels[0]]).Build())
			default:
				logger.Error("Unknown variable update operation", zap.String("operation", variableUpdate.Operation), zap.Any("alert", alert))
			}
		}
		results := valkeyClient.DoMulti(ctx, cmds...)
		for _, result := range results {
			if result.Error() != nil {
				switch {
				case valkey.IsValkeyNil(result.Error()):
					logger.Error("Valkey Nil Error", zap.Any("result", result.String()))
				default:
					logger.Error(err.Error(), zap.Any("result", result.String()))
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprint(w, SuccessResponse)
	}
}

func setupLogger(level zapcore.Level) {
	// Define custom encoder configuration
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"                   // Rename the time field
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder // Use human-readable timestamps
	encoderConfig.CallerKey = "caller"                    // Show caller file and line number
	// Create a core logger with the custom configuration
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig), // JSON logging with readable timestamps
		zapcore.Lock(os.Stdout),               // Output to stdout
		level,                                 // Log info and above
	)
	logger = zap.New(core, zap.AddCaller())
	defer func() {
		if err := logger.Sync(); err != nil {
			logger.Error("failed to sync zap logger", zap.Error(err))
		}
	}() // Flush logs before exiting
}

func processAlertsQueue(ctx context.Context, valkeyClient valkey.Client) {
	for {
		response, _ := valkeyClient.Do(ctx,
			valkeyClient.B().Xreadgroup().
				Group(ConsumerGroup, ConsumerName).
				Block(0).
				Streams().Key(StreamName).Id(">").
				Build(),
		).AsXRead()
		for _, v := range response {
			for _, vv := range v {
				var cmd valkey.Completed
				ack := valkeyClient.
					B().
					Xack().
					Key(StreamName).
					Group(ConsumerGroup).
					Id(vv.ID).
					Build()
				switch vv.FieldValues["action"] {
				case AddElement:
					cmd = valkeyClient.
						B().
						Sadd().
						Key(vv.FieldValues["key"]).
						Member(vv.FieldValues["value"]).
						Build()
				case RemoveElement:
					cmd = valkeyClient.
						B().
						Srem().
						Key(vv.FieldValues["key"]).
						Member(vv.FieldValues["value"]).
						Build()
				case ReplaceValue:
					cmd = valkeyClient.
						B().
						Set().
						Key(vv.FieldValues["key"]).
						Value(vv.FieldValues["value"]).
						Build()
				}
				valkeyClient.DoMulti(ctx, cmd, ack)
			}
		}
	}
}
