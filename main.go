package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/decisiveai/event-handler-webservice/eventing"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"sync"
	"time"

	"github.com/go-logr/zapr"
	"github.com/prometheus/alertmanager/template"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/contrib/bridges/otelzap"

	"github.com/cenkalti/backoff/v5"

	"github.com/decisiveai/mdai-data-core/audit"
	datacore "github.com/decisiveai/mdai-data-core/variables"
	mdaiv1 "github.com/decisiveai/mdai-operator/api/v1"

	"github.com/valkey-io/valkey-go"
)

const (
	valkeyEndpointEnvVarKey            = "VALKEY_ENDPOINT"
	valkeyPasswordEnvVarKey            = "VALKEY_PASSWORD"
	valkeyAuditStreamExpiryMSEnvVarKey = "VALKEY_AUDIT_STREAM_EXPIRY_MS"
	httpPortEnvVarKey                  = "HTTP_PORT"
	otelSdkDisabledEnvVar              = "OTEL_SDK_DISABLED"
	otelExporterOtlpEndpointEnvVar     = "OTEL_EXPORTER_OTLP_ENDPOINT"

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
	otelSdkEnabledStr := os.Getenv(otelSdkDisabledEnvVar)
	otelSdkEnabled := otelSdkEnabledStr != "true"
	otlpEndpointStr := os.Getenv(otelExporterOtlpEndpointEnvVar)
	if otelSdkEnabledStr == "" && otlpEndpointStr == "" {
		logger.Warn("No OTLP endpoint is defined, but OTEL SDK is enabled. Please set either " + otelSdkDisabledEnvVar + " or " + otelExporterOtlpEndpointEnvVar + " environment variable. You will receive 'connection refused' logs until this is resolved.")
	}
	// Set up OpenTelemetry.
	otelShutdown, err := setupOTelSDK(ctx, internalLogger, otelSdkEnabled)
	if err != nil {
		logger.Error("Error setting up OpenTelemetry SDK. Set "+otelSdkDisabledEnvVar+` to "true" to bypass this.`, zap.Error(err))
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

	auditAdapter := audit.NewAuditAdapter(zapr.NewLogger(logger), valkeyClient, valkeyAuditStreamExpiry)

	http.HandleFunc("/alerts", handleAlertsPost())
	http.HandleFunc("/audit", auditAdapter.HandleEventsGet(ctx))
	http.HandleFunc("/events", handleEventsPost())

	logger.Info("Starting server", zap.String("address", ":"+httpPort))
	logger.Fatal("failed to start server", zap.Error(http.ListenAndServe(":"+httpPort, nil)))
}

func getEnvVariableWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func createEventUuid() (string, error) {
	uuidBytes, err := exec.Command("uuidgen").Output()
	if err != nil {
		// TODO: Use our logger
		log.Fatal(err)
		return "", err
	}

	return string(uuidBytes), nil
}

func adaptPrometheusAlertToMdaiEvents(payload types.AlertManagerPayload) []types.MdaiEvent {
	sort.Slice(payload.Alerts, func(i, j int) bool {
		return payload.Alerts[i].StartsAt.Before(payload.Alerts[j].StartsAt)
	})

	mdaiEvents := make([]types.MdaiEvent, 0)

	for _, alert := range payload.Alerts {
		annotations := alert.Annotations
		labels := alert.Labels
		status := alert.Status

		unMarshalledPayload := make(map[string]interface{})
		for key, value := range labels {
			unMarshalledPayload[key] = value
		}
		unMarshalledPayload["value"] = annotations["current_value"]
		unMarshalledPayload["hubName"] = annotations["hub_name"]

		payloadBytes, err := json.Marshal(unMarshalledPayload)
		if err != nil {
			// TODO: Use our logger
			log.Fatal(err)
		}

		var Id string
		if alert.Fingerprint != "" {
			Id = alert.Fingerprint
		} else {
			uuid, err := createEventUuid()
			if err == nil {
				Id = uuid
			}
		}

		mdaiEvent := types.MdaiEvent{
			Name:      annotations["alert_name"] + "." + status,
			Source:    "prometheus",
			Id:        Id,
			Timestamp: alert.StartsAt.String(),
			Payload:   string(payloadBytes),
		}

		// TODO: Log event created
		mdaiEvents = append(mdaiEvents, mdaiEvent)
	}

	return mdaiEvents
}

func handleAlertsPost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
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

		mdaiEvents := adaptPrometheusAlertToMdaiEvents(payload)

		for _, mdaiEvent := range mdaiEvents {
			eventing.EmitMdaiEvent(mdaiEvent)
		}
	}
}

func handleEventsPost() http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
			return
		}

		// Read and parse the request body
		var event types.MdaiEvent
		decoder := json.NewDecoder(r.Body)
		err := decoder.Decode(&event)

		// Check for JSON decoding errors
		if err != nil {
			http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
			return
		}

		if event.Id == "" {
			uuid, err := createEventUuid()
			if err == nil {
				event.Id = uuid
			}
		}

		eventing.EmitMdaiEvent(event)

		// TODO: Send correct response
		w.WriteHeader(http.StatusCreated)
		w.Write([]byte("Event received successfully"))

	}
}
