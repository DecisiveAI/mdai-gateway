package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"time"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/decisiveai/mdai-gateway/types"

	"github.com/prometheus/alertmanager/template"

	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/clientcmd"

	"go.opentelemetry.io/contrib/bridges/otelzap"

	"github.com/cenkalti/backoff/v5"

	"github.com/decisiveai/mdai-data-core/audit"

	"github.com/valkey-io/valkey-go"
)

const (
	valkeyEndpointEnvVarKey            = "VALKEY_ENDPOINT"
	valkeyPasswordEnvVarKey            = "VALKEY_PASSWORD"
	valkeyAuditStreamExpiryMSEnvVarKey = "VALKEY_AUDIT_STREAM_EXPIRY_MS"

	rabbitmqEndpointEnvVarKey = "RABBITMQ_ENDPOINT"
	rabbitmqPasswordEnvVarKey = "RABBITMQ_PASSWORD"

	httpPortEnvVarKey              = "HTTP_PORT"
	defaultHttpPort                = "8081"
	otelSdkDisabledEnvVar          = "OTEL_SDK_DISABLED"
	otelExporterOtlpEndpointEnvVar = "OTEL_EXPORTER_OTLP_ENDPOINT"

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
	otelCore := otelzap.NewCore("github.com/decisiveai/mdai-gateway")
	multiCore := zapcore.NewTee(core, otelCore)
	logger = zap.New(multiCore, zap.AddCaller())
	// don't really care about failing of defer that is the last thing run before the program exists
	//nolint:all
	defer logger.Sync() // Flush logs before exiting
}

func createK8sClient() (dynamic.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig, err := os.UserHomeDir()
		if err != nil {
			logger.Error("Failed to load k8s config", zap.Error(err))
			return nil, err
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig+"/.kube/config")
		if err != nil {
			logger.Error("Failed to build k8s config", zap.Error(err))
			return nil, err
		}
	}
	return dynamic.NewForConfig(config)
}

func main() {
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

	valkeyClient := initValkey(ctx)
	defer valkeyClient.Close()

	hub := initRmq(ctx)
	defer hub.Close()

	k8sClient, err := createK8sClient()
	if err != nil {
		logger.Fatal("failed to create k8s client", zap.Error(err))
	}

	router := http.NewServeMux()

	router.HandleFunc("/events", handleEventsRoute(ctx, valkeyClient, hub))
	router.HandleFunc("/variables/list/hub/{hubName}/", HandleListVariables(ctx, k8sClient))
	router.HandleFunc("/variables/list/", HandleListVariables(ctx, k8sClient))
	router.HandleFunc("/variables/values/hub/{hubName}/var/{varName}/", HandleGetVariables(ctx, valkeyClient, k8sClient))
	router.HandleFunc("/variables/hub/{hubName}/var/{varName}/", HandleSetDeleteVariables(ctx, k8sClient, hub))

	logger.Info("Starting server", zap.String("address", ":"+httpPort))
	logger.Fatal("failed to start server", zap.Error(http.ListenAndServe(":"+httpPort, router)))
}

func initRmq(ctx context.Context) eventing.EventHubInterface {
	retryCount := 0
	connectToRmq := func() (eventing.EventHubInterface, error) {
		rmqPassword := getEnvVariableWithDefault(rabbitmqPasswordEnvVarKey, "")
		rmqEndpoint := getEnvVariableWithDefault(rabbitmqEndpointEnvVarKey, "localhost:5672")

		hubUrl := (&url.URL{
			Scheme: "amqp",
			Host:   rmqEndpoint,
			User:   url.UserPassword("mdai", rmqPassword),
		}).String()

		hub, err := eventing.NewEventHub(hubUrl, eventing.EventQueueName, logger)
		if err != nil {
			retryCount++
			return nil, err
		}
		logger.Info("Successfully created EventHub", zap.String("Endpoint", rmqEndpoint))
		return hub, nil
	}

	notifyFunc := func(err error, duration time.Duration) {
		logger.Warn("failed to initialize rmq. retrying...",
			zap.Error(err),
			zap.Int("retry_count", retryCount),
			zap.Duration("duration", duration))
	}

	hub, err := backoff.Retry(
		ctx,
		connectToRmq,
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
		backoff.WithMaxElapsedTime(3*time.Minute),
		backoff.WithNotify(notifyFunc),
	)
	if err != nil {
		logger.Fatal("failed to connect to rmq", zap.Error(err))
	}
	return hub
}

func initValkey(ctx context.Context) valkey.Client {
	retryCount := 0
	connectToValkey := func() (valkey.Client, error) {
		var err error
		client, err := valkey.NewClient(valkey.ClientOption{
			InitAddress: []string{getEnvVariableWithDefault(valkeyEndpointEnvVarKey, "mdai-valkey-primary.mdai.svc.cluster.local:6379")},
			Password:    getEnvVariableWithDefault(valkeyPasswordEnvVarKey, "abc"),
		})
		if err != nil {
			retryCount++
			return nil, err
		}
		return client, nil
	}
	exponentialBackoff := backoff.NewExponentialBackOff()
	exponentialBackoff.InitialInterval = 5 * time.Second

	notifyFunc := func(err error, duration time.Duration) {
		logger.Warn("failed to initialize valkey. retrying...",
			zap.Error(err),
			zap.Int("retry_count", retryCount),
			zap.Duration("duration", duration))
	}

	valkeyClient, err := backoff.Retry(
		ctx,
		connectToValkey,
		backoff.WithBackOff(backoff.NewExponentialBackOff()),
		backoff.WithMaxElapsedTime(3*time.Minute),
		backoff.WithNotify(notifyFunc),
	)

	if err != nil {
		logger.Fatal("failed to get valkey client", zap.Error(err))
	}

	return valkeyClient
}

func getEnvVariableWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
func handleEventsRoute(ctx context.Context, valkeyClient valkey.Client, hub eventing.EventHubInterface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		logger.Info("handling request ", zap.Any("request", r))
		if r.Method == http.MethodGet {
			eventsMap, err := audit.NewAuditAdapter(logger, valkeyClient, valkeyAuditStreamExpiry).HandleEventsGet(ctx)
			if err != nil {
				logger.Error("failed to get events", zap.Error(err))
				http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
				return
			}

			resultMapJson, err := json.Marshal(eventsMap)
			if err != nil {
				logger.Error("failed to marshal events map to json", zap.Error(err))
				http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
				return
			}

			w.Header().Set("Content-Type", "application/json")
			w.WriteHeader(http.StatusOK)
			if _, err := w.Write(resultMapJson); err != nil {
				logger.Error("Failed to write response body", zap.Error(err))
			}
			return
		}

		if r.Method == http.MethodPost {
			bodyBytes, err := io.ReadAll(r.Body)
			if err != nil {
				logger.Error("Failed to read request body", zap.Error(err))
				http.Error(w, "Failed to read request body: "+err.Error(), http.StatusBadRequest)
				return
			}

			logger.Debug("Received POST request body", zap.ByteString("bodyBytes", bodyBytes))

			// Try to determine the payload type by checking for key fields
			var rawJson map[string]interface{}
			if err := json.Unmarshal(bodyBytes, &rawJson); err != nil {
				logger.Error("Failed to parse JSON", zap.Error(err))
				http.Error(w, "Invalid JSON in request body", http.StatusBadRequest)
				return
			}

			// Check if it looks like a Prometheus alert
			if _, hasReceiver := rawJson["receiver"]; hasReceiver {
				if _, hasAlerts := rawJson["alerts"]; hasAlerts {
					handlePrometheusAlert(w, bodyBytes, hub, logger)
					return
				}
			}

			// Check if it looks like an MdaiEvent
			if _, hasName := rawJson["name"]; hasName {
				if _, hasPayload := rawJson["payload"]; hasPayload {
					handleMdaiEvent(w, bodyBytes, hub, logger)
					return
				}
			}

			// If we got here, we couldn't identify the payload format
			logger.Warn("Unrecognized payload format", zap.Any("rawJson", rawJson))
			http.Error(w, "Invalid request body format", http.StatusBadRequest)
			return
		}

		// Handle any other HTTP methods
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
	}
}

// Handle Prometheus Alertmanager alerts
func handlePrometheusAlert(w http.ResponseWriter, bodyBytes []byte, hub eventing.EventHubInterface, logger *zap.Logger) {
	var alertData template.Data
	if err := json.Unmarshal(bodyBytes, &alertData); err != nil {
		logger.Error("Failed to unmarshal Prometheus alert", zap.Error(err))
		http.Error(w, "Invalid Prometheus alert format: "+err.Error(), http.StatusBadRequest)
		return
	}

	logger.Info("Processing Prometheus alert",
		zap.String("receiver", alertData.Receiver),
		zap.String("status", alertData.Status),
		zap.Int("alertCount", len(alertData.Alerts)))

	events := types.AdaptPrometheusAlertToMdaiEvents(alertData)

	successCount := 0
	for _, event := range events {
		if err := hub.PublishMessage(event); err != nil {
			logger.Warn("Failed to publish event",
				zap.String("event_name", event.Name),
				zap.Error(err))
		} else {
			successCount++
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	response := map[string]interface{}{
		"message":    "Processed Prometheus alerts",
		"total":      len(events),
		"successful": successCount,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to write response", zap.Error(err))
	}
}

// Handle direct MdaiEvent submissions
func handleMdaiEvent(w http.ResponseWriter, bodyBytes []byte, hub eventing.EventHubInterface, logger *zap.Logger) {
	var event eventing.MdaiEvent
	if err := json.Unmarshal(bodyBytes, &event); err != nil {
		logger.Error("Failed to unmarshal MdaiEvent", zap.Error(err))
		http.Error(w, "Invalid MdaiEvent format: "+err.Error(), http.StatusBadRequest)
		return
	}

	// Validate required fields
	if event.Name == "" {
		logger.Warn("Missing required field: name")
		http.Error(w, "Missing required field: name", http.StatusBadRequest)
		return
	}

	if event.HubName == "" {
		logger.Warn("Missing required field: hubName")
		http.Error(w, "Missing required field: hubName", http.StatusBadRequest)
		return
	}

	if event.Payload == "" {
		logger.Warn("Missing required field: payload")
		http.Error(w, "Missing required field: payload", http.StatusBadRequest)
		return
	}

	// Generate ID if not provided
	if event.Id == "" {
		event.Id = types.CreateEventUuid()
	}

	// Set timestamp if not provided
	if event.Timestamp.IsZero() {
		event.Timestamp = time.Now()
	}

	logger.Info("Processing MdaiEvent",
		zap.String("id", event.Id),
		zap.String("name", event.Name),
		zap.String("source", event.Source))

	if err := hub.PublishMessage(event); err != nil {
		logger.Error("Failed to publish MdaiEvent", zap.Error(err))
		http.Error(w, fmt.Sprintf("Failed to publish event: %v", err), http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	response := map[string]interface{}{
		"message": "Event received successfully",
		"id":      event.Id,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to write response", zap.Error(err))
	}
}
