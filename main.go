package main

import (
	"context"
	"encoding/json"
	"fmt"
	"go.uber.org/multierr"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"github.com/cenkalti/backoff/v4"

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

	mdaiHubEventHistoryStreamName = "mdai_hub_event_history"
)

var processMutex sync.Mutex

var logger *zap.Logger

func init() {
	// Define custom encoder configuration
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"                   // Rename the time field
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder // Use human-readable timestamps
	encoderConfig.CallerKey = "caller"                    // Show caller file and line number
	// Create a core logger with the custom configuration
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig), // JSON logging with readable timestamps
		zapcore.Lock(os.Stdout),               // Output to stdout
		zap.DebugLevel,                        // Log info and above
	)
	logger = zap.New(core, zap.AddCaller())
	defer logger.Sync() // Flush logs before exiting
}

func main() {
	var valkeyClient valkey.Client
	ctx := context.Background()

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

	http.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))
	http.HandleFunc("/events", handleEventsGet(ctx, valkeyClient))

	logger.Info("Starting server", zap.String("address", ":8081"))
	logger.Fatal("failed to start server", zap.Error(http.ListenAndServe(":8081", nil)))
}

func getEnvVariableWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func handleEventsGet(ctx context.Context, valkeyClient valkey.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result := valkeyClient.Do(ctx, valkeyClient.B().Xrange().Key(mdaiHubEventHistoryStreamName).Start("-").End("+").Build())
		err := result.Error()
		if err != nil {
			logger.Error("valkey error", zap.Error(err))
			http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
			return
		}
		resultList, err := result.ToArray()
		if err != nil {
			logger.Error("failed to get valkey value as map", zap.Error(err))
			http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
			return
		}
		entries := make([]map[string]string, 0)
		for _, entry := range resultList {
			entryMap, err := entry.AsXRangeEntry()
			if err != nil {
				logger.Error("failed to convert entry to map", zap.Error(err))
				continue
			}
			entries = append(entries, entryMap.FieldValues)
		}
		resultMapJson, err := json.Marshal(entries)
		if err != nil {
			logger.Error("failed to marshal events map to json", zap.Error(err))
			http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(resultMapJson); err != nil {
			logger.Error("Failed to write response body (y tho)", zap.Error(err))
		}
	}
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

		var valkeyErrors error
		for _, alert := range payload.Alerts {
			hubName := alert.Annotations[HubName]
			if hubName == "" {
				logger.Info("Skipping alert because no hub_name found in alert annotations, payload: %v", zap.Any("alert", alert))
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

			// next time, valkeyKeyKey!
			valkeyKey := VariableKeyPrefix + hubName + "/" + variableUpdate.VariableRef
			switch variableUpdate.Operation {
			case AddElement:
				for _, element := range relevantLabels {
					addOperationCommand := valkeyClient.B().Sadd().Key(valkeyKey).Member(alert.Labels[element]).Build()
					mdaiHubEvent := createHubEvent(relevantLabels, variableUpdate, valkeyKey, alert)
					err := doVariableUpdateAndLog(ctx, valkeyClient, addOperationCommand, mdaiHubEvent, valkeyKey)
					valkeyErrors = multierr.Append(valkeyErrors, err)
				}
			case RemoveElement:
				for _, element := range relevantLabels {
					removeOperationCommand := valkeyClient.B().Srem().Key(valkeyKey).Member(alert.Labels[element]).Build()
					mdaiHubEvent := createHubEvent(relevantLabels, variableUpdate, valkeyKey, alert)
					err := doVariableUpdateAndLog(ctx, valkeyClient, removeOperationCommand, mdaiHubEvent, valkeyKey)
					valkeyErrors = multierr.Append(valkeyErrors, err)
				}
			case ReplaceValue:
				if len(relevantLabels) > 1 {
					logger.Info("Multiple relevantLabels found for replace action",
						zap.String("selected_label", relevantLabels[0]),
						zap.Any("all_labels", relevantLabels),
					)
				}
				replaceOperationCommand := valkeyClient.B().Set().Key(valkeyKey).Value(alert.Labels[relevantLabels[0]]).Build()
				mdaiHubEvent := createHubEvent(relevantLabels, variableUpdate, valkeyKey, alert)
				err := doVariableUpdateAndLog(ctx, valkeyClient, replaceOperationCommand, mdaiHubEvent, valkeyKey)
				valkeyErrors = multierr.Append(valkeyErrors, err)
			default:
				logger.Error("Unknown variable update operation", zap.String("operation", variableUpdate.Operation), zap.Any("alert", alert))
			}
		}

		if valkeyErrors != nil {
			logger.Error("Errors occurred writing updates to valkey", zap.Error(valkeyErrors))
			http.Error(w, "valkey errors: "+valkeyErrors.Error(), http.StatusInternalServerError)
		} else {
			logger.Info("Successfully wrote all variable updates")
			w.WriteHeader(http.StatusOK)
			fmt.Fprintf(w, `{"success": "variable(s) updated"}`)
		}
	}
}

func doVariableUpdateAndLog(ctx context.Context, valkeyClient valkey.Client, variableUpdateCommand valkey.Completed, mdaiHubEvent types.MdaiHubEvent, valkeyKey string) error {
	logger.Info("Performing "+mdaiHubEvent.Operation+" operation",
		zap.String("variable", valkeyKey),
		zap.Any("mdaiHubEvent", mdaiHubEvent),
	)
	auditLogCommand := makeAuditLogCommand(valkeyClient, mdaiHubEvent)
	results := valkeyClient.DoMulti(ctx,
		variableUpdateCommand,
		auditLogCommand,
	)
	valkeyMultiErr := accumulateValkeyErrors(results)
	return valkeyMultiErr
}

func accumulateValkeyErrors(results []valkey.ValkeyResult) error {
	var valkeyMultiErr error
	for _, result := range results {
		valkeyError := result.Error()
		if valkeyError != nil {
			valkeyMultiErr = multierr.Append(valkeyMultiErr, valkeyError)
			logger.Error("Valkey error", zap.Error(valkeyError))
		}
	}
	return valkeyMultiErr
}

func makeAuditLogCommand(valkeyClient valkey.Client, mdaiHubEvent types.MdaiHubEvent) valkey.Completed {
	return valkeyClient.B().Xadd().Key(mdaiHubEventHistoryStreamName).Minid().Threshold(getAuditLogTTLMinId()).Id("*").FieldValue().FieldValueIter(mdaiHubEvent.ToSequence()).Build()
}

func createHubEvent(relevantLabels []string, variableUpdate *mdaiv1.VariableUpdate, valkeyKey string, alert types.Alert) types.MdaiHubEvent {
	mdaiHubEvent := types.MdaiHubEvent{
		Type:      "variable_updated",
		Operation: variableUpdate.Operation,
		Variable:  valkeyKey,
		Value:     alert.Labels[relevantLabels[0]],
	}
	return mdaiHubEvent
}

func getAuditLogTTLMinId() string {
	thirtyDaysThreshold := strconv.FormatInt(time.Now().Add(-30*24*time.Hour).UnixMilli(), 10)
	return thirtyDaysThreshold
}
