package otel

import (
	"context"
	"os"

	"go.uber.org/zap"
)

func Setup(ctx context.Context, logger *zap.Logger, internalLogger *zap.Logger) error {
	otelSdkEnabledStr := os.Getenv(SdkDisabledEnvVar)
	otelSdkEnabled := otelSdkEnabledStr != "true"
	otlpEndpointStr := os.Getenv(otelExporterOtlpEndpointEnvVar)
	if otelSdkEnabledStr == "" && otlpEndpointStr == "" {
		logger.Warn("No OTLP endpoint is defined, but OTEL SDK is enabled. Please set either " + SdkDisabledEnvVar + " or " + otelExporterOtlpEndpointEnvVar + " environment variable. You will receive 'connection refused' logs until this is resolved.")
	}

	// Initialize OpenTelemetry SDK
	if !otelSdkEnabled {
		logger.Info("OTEL SDK has been disabled with " + SdkDisabledEnvVar + " environment variable")
		return nil
	}

	otelShutdown, err := setupOTelSDK(ctx, internalLogger)
	if err != nil {
		logger.Error("Error setting up OpenTelemetry SDK. Set "+SdkDisabledEnvVar+` to "true" to bypass this.`, zap.Error(err))
		return err
	}

	// Graceful shutdown on context cancellation
	defer func() {
		if err := otelShutdown(ctx); err != nil {
			logger.Error("OTEL SDK did not shut down gracefully!", zap.Error(err))
		}
	}()

	return nil
}
