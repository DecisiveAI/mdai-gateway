package main

import (
	"context"
	"encoding/json"
	"github.com/decisiveai/event-handler-webservice/eventing"
	"github.com/decisiveai/event-handler-webservice/types"
	"github.com/go-logr/zapr"
	"io"
	"net/http"
	"os"
	"strconv"
	"time"

	"github.com/prometheus/alertmanager/template"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"

	"go.opentelemetry.io/contrib/bridges/otelzap"

	"github.com/cenkalti/backoff/v5"

	"github.com/decisiveai/mdai-data-core/audit"

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

	mdaiHubEventHistoryStreamName = "mdai_hub_event_history"
)

var (
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

	http.HandleFunc("/events", handleEventsRoute(ctx, valkeyClient))

	logger.Info("Starting server", zap.String("address", ":"+httpPort))
	logger.Fatal("failed to start server", zap.Error(http.ListenAndServe(":"+httpPort, nil)))
}

func getEnvVariableWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func handleEventsRoute(ctx context.Context, valkeyClient valkey.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			eventsMap, err := audit.NewAuditAdapter(zapr.NewLogger(logger), valkeyClient, valkeyAuditStreamExpiry).HandleEventsGet(ctx)

			resultMapJson, err := json.Marshal(eventsMap)
			if err != nil {
				logger.Error("failed to marshal events map to json")
				http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write(resultMapJson); err != nil {
				logger.Error("Failed to write response body (y tho)")
			}

			return
		}
		if r.Method == http.MethodPost {

			bodyBytes, err := io.ReadAll(r.Body)
			// Check for JSON decoding errors
			if err != nil {
				http.Error(w, "Invalid request body: "+err.Error(), http.StatusBadRequest)
				return
			}

			var alertData template.Data
			if err := json.Unmarshal(bodyBytes, &alertData); err == nil {
				events := types.AdaptPrometheusAlertToMdaiEvents(alertData)

				for _, event := range events {
					eventing.EmitMdaiEvent(event)
				}
				// TODO: Send correct response
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte("Events received successfully"))
				return
			}

			var event types.MdaiEvent
			if err := json.Unmarshal(bodyBytes, &event); err == nil {
				if event.Id == "" {
					uuid, err := types.CreateEventUuid()
					if err == nil {
						event.Id = uuid
					}
				}

				eventing.EmitMdaiEvent(event)
				// TODO: Send correct response
				w.WriteHeader(http.StatusCreated)
				w.Write([]byte("Event received successfully"))
				return
			}

			http.Error(w, "Invalid request body", http.StatusBadRequest)
		} else {
			http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
			return
		}

	}
}
