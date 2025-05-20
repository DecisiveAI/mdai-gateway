package main

import (
	"context"
	"net/http"
	"os"
	"strconv"
	"sync"
	"time"

	"github.com/decisiveai/event-handler-webservice/internal/otel"
	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/go-logr/zapr"
	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

var (
	processMutex            sync.Mutex
	valkeyAuditStreamExpiry = 30 * 24 * time.Hour
)

func main() {
	internalLogger, logger, cleanup := setupLogger()
	defer cleanup()

	ctx := context.Background()
	otelSdkEnabledStr := os.Getenv(otelSdkDisabledEnvVar)
	otelSdkEnabled := otelSdkEnabledStr != "true"
	otlpEndpointStr := os.Getenv(otelExporterOtlpEndpointEnvVar)
	if otelSdkEnabledStr == "" && otlpEndpointStr == "" {
		logger.Warn("No OTLP endpoint is defined, but OTEL SDK is enabled. Please set either " + otelSdkDisabledEnvVar + " or " + otelExporterOtlpEndpointEnvVar + " environment variable. You will receive 'connection refused' logs until this is resolved.")
	}

	// Set up OpenTelemetry.
	otelShutdown, err := otel.SetupOTelSDK(ctx, internalLogger, otelSdkEnabled)
	if err != nil {
		logger.Error("Error setting up OpenTelemetry SDK. Set "+otelSdkDisabledEnvVar+` to "true" to bypass this.`, zap.Error(err))
		os.Exit(1)
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

	valkeyClient := getValkeyClient(ctx, logger)
	auditAdapter := audit.NewAuditAdapter(zapr.NewLogger(logger), valkeyClient, valkeyAuditStreamExpiry)

	http.HandleFunc("/alerts", handleAlertsPost(ctx, logger, valkeyClient))
	http.HandleFunc("/events", auditAdapter.HandleEventsGet(ctx))

	logger.Info("Starting server", zap.String("address", ":"+httpPort))
	logger.Fatal("failed to start server", zap.Error(http.ListenAndServe(":"+httpPort, nil)))
}

func getEnvVariableWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func setupLogger() (internalLogger, logger *zap.Logger, cleanup func()) {
	// Define custom encoder configuration
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"                   // Rename the time field
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder // Use human-readable timestamps
	encoderConfig.CallerKey = "caller"                    // Show caller file and line number
	baseCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig), // JSON logging with readable timestamps
		zapcore.Lock(os.Stdout),               // Output to stdout
		zap.DebugLevel,                        // Log info and above
	)
	otelCore := otelzap.NewCore(otelZapCoreName)
	multiCore := zapcore.NewTee(baseCore, otelCore)

	internalLogger = zap.New(baseCore, zap.AddCaller())
	logger = zap.New(multiCore, zap.AddCaller())

	return internalLogger, logger, func() { _ = internalLogger.Sync(); _ = logger.Sync() }
}
