package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/zapr"
	"github.com/prometheus/alertmanager/template"
	"go.uber.org/multierr"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/contrib/bridges/otelzap"

	"github.com/cenkalti/backoff/v5"

	datacore "github.com/decisiveai/mdai-data-core/variables"
	mdaiv1 "github.com/decisiveai/mdai-operator/api/v1"

	mdaiAuditStream "github.com/decisiveai/event-handler-webservice/audit"
	"github.com/valkey-io/valkey-go"
)

const (
	valkeyEndpointEnvVarKey            = "VALKEY_ENDPOINT"
	valkeyPasswordEnvVarKey            = "VALKEY_PASSWORD"
	valkeyAuditStreamExpiryMSEnvVarKey = "VALKEY_AUDIT_STREAM_EXPIRY_MS"
	httpPortEnvVarKey                  = "HTTP_PORT"

	defaultHttpPort = "8081"

	firingStatus   = "firing"
	resolvedStatus = "resolved"

	actionContextAnnotationsKey  = "action_context"
	relevantLabelsAnnotationsKey = "relevant_labels"
	HubName                      = "hub_name"

	mdaiHubEventHistoryStreamName = "mdai_hub_event_history"
)

var (
	processMutex sync.Mutex
	// Intended ONLY for use by the OTEL SDK, use logger for all other purposes
	internalLogger          *zap.Logger
	logger                  *zap.Logger
	valkeyAuditStreamExpiry = 30 * 24 * time.Hour
)

func init() {
	// Define custom encoder configuration
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"                   // Rename the time field
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder // Use human-readable timestamps
	encoderConfig.CallerKey = "caller"                    // Show caller file and line number
	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig), // JSON logging with readable timestamps
		zapcore.Lock(os.Stdout),               // Output to stdout
		zap.DebugLevel,                        // Log info and above
	)
	internalLogger = zap.New(core, zap.AddCaller())
	// don't really care about failing of defer that is the last thing run before the program exists
	//nolint:all
	defer internalLogger.Sync() // Flush logs before exiting
	otelCore := otelzap.NewCore("github.com/decisiveai/event-handler-webservice")
	multiCore := zapcore.NewTee(core, otelCore)
	logger = zap.New(multiCore, zap.AddCaller())
	// don't really care about failing of defer that is the last thing run before the program exists
	//nolint:all
	defer logger.Sync() // Flush logs before exiting
}

func main() {
	var (
		valkeyClient valkey.Client
		retryCount   int
	)

	ctx := context.Background()

	// Set up OpenTelemetry.
	otelShutdown, err := setupOTelSDK(ctx, internalLogger)
	if err != nil {
		logger.Error("Error setting up otel client", zap.Error(err))
		return
	}
	defer func() {
		if err := otelShutdown(ctx); err != nil {
			logger.Error("OTEL SDK did not shut down gracefully!", zap.Error(err))
		}
	}()

	httpPort := getEnvVariableWithDefault(httpPortEnvVarKey, defaultHttpPort)
	valkeyStreamExpiryMsStr := os.Getenv(valkeyAuditStreamExpiryMSEnvVarKey)
	if valkeyStreamExpiryMsStr != "" {
		envExpiryMs, err := strconv.Atoi(valkeyStreamExpiryMsStr)
		if err != nil {
			logger.Fatal("Failed to parse valkeyStreamExpiryMs env var", zap.Error(err))
			return
		}
		valkeyAuditStreamExpiry = time.Duration(envExpiryMs) * time.Millisecond
		logger.Info("Using custom "+mdaiHubEventHistoryStreamName+" expiration threshold MS", zap.Int64("valkeyAuditStreamExpiryMs", valkeyAuditStreamExpiry.Milliseconds()))

	}

	operation := func() (string, error) {
		var err error
		valkeyClient, err = valkey.NewClient(valkey.ClientOption{
			InitAddress: []string{getEnvVariableWithDefault(valkeyEndpointEnvVarKey, "")},
			Password:    getEnvVariableWithDefault(valkeyPasswordEnvVarKey, ""),
		})
		if err != nil {
			retryCount++
			return "", err
		}
		return "", nil
	}
	exponentialBackoff := backoff.NewExponentialBackOff()
	exponentialBackoff.InitialInterval = 5 * time.Second

	notifyFunc := func(err error, duration time.Duration) {
		logger.Warn("failed to initialize valkey client. retrying...", zap.Int("retry_count", retryCount), zap.Duration("duration", duration))
	}

	if _, err := backoff.Retry(ctx, operation,
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
		backoff.WithMaxElapsedTime(3*time.Minute),
		backoff.WithNotify(notifyFunc)); err != nil {
		logger.Fatal("failed to get valkey client", zap.Error(err))
	}

	adapter := mdaiAuditStream.NewAuditAdapter(logger, valkeyClient, valkeyAuditStreamExpiry)

	http.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))
	http.HandleFunc("/events", adapter.HandleEventsGet(ctx, valkeyClient))

	logger.Info("Starting server", zap.String("address", ":"+httpPort))
	logger.Fatal("failed to start server", zap.Error(http.ListenAndServe(":"+httpPort, nil)))
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

		var payload template.Data
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
			valkeyKey := datacore.ComposeValkeyKey(hubName, variableUpdate.VariableRef)
			adapter := mdaiAuditStream.NewAuditAdapter(logger, valkeyClient, valkeyAuditStreamExpiry)
			dataAdapter := datacore.NewValkeyAdapter(valkeyClient, zapr.NewLogger(logger))

			mdaiHubEvent := adapter.CreateHubEvent(relevantLabels, alert)
			err := adapter.InsertAuditLogEvent(ctx, valkeyClient, mdaiHubEvent)
			if err != nil {
				valkeyErrors = multierr.Append(valkeyErrors, err)
			}

			switch variableUpdate.Operation {
			case mdaiv1.VariableUpdateSetAddElement:
				for _, element := range relevantLabels {
					addOperationCommand := dataAdapter.AddElementToSet(valkeyKey, alert.Labels[element])
					mdaiHubAction := adapter.CreateHubAction(relevantLabels, variableUpdate, valkeyKey, alert)
					err := adapter.DoVariableUpdateAndLog(ctx, valkeyClient, addOperationCommand, mdaiHubAction, valkeyKey)
					valkeyErrors = multierr.Append(valkeyErrors, err)
				}
			case mdaiv1.VariableUpdateSetRemoveElement:
				for _, element := range relevantLabels {
					removeOperationCommand := dataAdapter.RemoveElementFromSet(valkeyKey, alert.Labels[element])
					mdaiHubAction := adapter.CreateHubAction(relevantLabels, variableUpdate, valkeyKey, alert)
					err := adapter.DoVariableUpdateAndLog(ctx, valkeyClient, removeOperationCommand, mdaiHubAction, valkeyKey)
					valkeyErrors = multierr.Append(valkeyErrors, err)
				}
			case mdaiv1.VariableUpdateSet:
				if len(relevantLabels) > 1 {
					logger.Info("Multiple relevantLabels found for replace action",
						zap.String("selected_label", relevantLabels[0]),
						zap.Any("all_labels", relevantLabels),
					)
				}
				setOperationCommand := dataAdapter.SetString(valkeyKey, alert.Labels[relevantLabels[0]])
				mdaiHubAction := adapter.CreateHubAction(relevantLabels, variableUpdate, valkeyKey, alert)
				err := adapter.DoVariableUpdateAndLog(ctx, valkeyClient, setOperationCommand, mdaiHubAction, valkeyKey)
				valkeyErrors = multierr.Append(valkeyErrors, err)
			case mdaiv1.VariableUpdateDelete:
				deleteOperationCommand := dataAdapter.DeleteString(valkeyKey)
				mdaiHubAction := adapter.CreateHubAction(relevantLabels, variableUpdate, valkeyKey, alert)
				err := adapter.DoVariableUpdateAndLog(ctx, valkeyClient, deleteOperationCommand, mdaiHubAction, valkeyKey)
				valkeyErrors = multierr.Append(valkeyErrors, err)
			case mdaiv1.VariableUpdateIntIncrBy:
				// increment by the number of relevant labels
				intIncrByOperationCommand := dataAdapter.IntIncrBy(valkeyKey, int64(len(relevantLabels)))
				mdaiHubAction := adapter.CreateHubAction(relevantLabels, variableUpdate, valkeyKey, alert)
				err := adapter.DoVariableUpdateAndLog(ctx, valkeyClient, intIncrByOperationCommand, mdaiHubAction, valkeyKey)
				valkeyErrors = multierr.Append(valkeyErrors, err)
			case mdaiv1.VariableUpdateIntDecrBy:
				// decrement by the number of relevant labels
				intDecrByOperationCommand := dataAdapter.IntDecrBy(valkeyKey, int64(len(relevantLabels)))
				mdaiHubAction := adapter.CreateHubAction(relevantLabels, variableUpdate, valkeyKey, alert)
				err := adapter.DoVariableUpdateAndLog(ctx, valkeyClient, intDecrByOperationCommand, mdaiHubAction, valkeyKey)
				valkeyErrors = multierr.Append(valkeyErrors, err)
			case mdaiv1.VariableUpdateSetMapEntry:
				if len(relevantLabels) > 1 {
					logger.Info("Multiple relevantLabels found for SetKey action",
						zap.String("selected_label", relevantLabels[0]),
						zap.Any("all_labels", relevantLabels),
					)
				}
				setKeyOperationCommand := dataAdapter.SetMapEntry(valkeyKey, relevantLabels[0], alert.Labels[relevantLabels[0]])
				mdaiHubAction := adapter.CreateHubAction(relevantLabels, variableUpdate, valkeyKey, alert)
				err := adapter.DoVariableUpdateAndLog(ctx, valkeyClient, setKeyOperationCommand, mdaiHubAction, valkeyKey)
				valkeyErrors = multierr.Append(valkeyErrors, err)
			case mdaiv1.VariableUpdateRemoveMapEntry:
				removeKeyOperationCommand := dataAdapter.RemoveMapEntry(valkeyKey, relevantLabels[0])
				mdaiHubAction := adapter.CreateHubAction(relevantLabels, variableUpdate, valkeyKey, alert)
				err := adapter.DoVariableUpdateAndLog(ctx, valkeyClient, removeKeyOperationCommand, mdaiHubAction, valkeyKey)
				valkeyErrors = multierr.Append(valkeyErrors, err)
			case mdaiv1.VariableUpdateBulkSetKeyValue:
				for i, label := range relevantLabels {
					setKeyOperationCommand := dataAdapter.SetMapEntry(valkeyKey, relevantLabels[i], alert.Labels[label])
					mdaiHubAction := adapter.CreateHubAction(relevantLabels, variableUpdate, valkeyKey, alert)
					err := adapter.DoVariableUpdateAndLog(ctx, valkeyClient, setKeyOperationCommand, mdaiHubAction, valkeyKey)
					valkeyErrors = multierr.Append(valkeyErrors, err)
				}
			default:
				logger.Error("Unknown variable update operation", zap.String("operation", string(variableUpdate.Operation)), zap.Any("alert", alert))
			}
		}

		if valkeyErrors != nil {
			logger.Error("Errors occurred writing updates to valkey", zap.Error(valkeyErrors))
			http.Error(w, "valkey errors: "+valkeyErrors.Error(), http.StatusInternalServerError)
			return
		}

		logger.Info("Successfully wrote all variable updates")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"success": "variable(s) updated"}`)
	}
}
