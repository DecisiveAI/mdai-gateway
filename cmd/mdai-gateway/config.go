package main

import (
	"os"

	"github.com/decisiveai/mdai-data-core/helpers"
	"go.uber.org/zap"
)

type Config struct {
	HTTPPort     string
	OTelEndpoint string
	OTelDisabled bool
}

func loadConfig(logger *zap.Logger) *Config {
	otelDisabledStr := os.Getenv(otelSdkDisabledEnvVar)
	otelEndpointStr := os.Getenv(otelExporterOtlpEndpointEnvVar)

	if otelDisabledStr == "" && otelEndpointStr == "" {
		logger.Warn("Warning: No OTLP endpoint is defined, but OTEL SDK is enabled.")
	}

	cfg := &Config{
		HTTPPort:     helpers.GetEnvVariableWithDefault(httpPortEnvVarKey, defaultHTTPPort),
		OTelDisabled: otelDisabledStr == "true",
		OTelEndpoint: otelEndpointStr,
	}
	return cfg
}
