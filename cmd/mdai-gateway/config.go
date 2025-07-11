package main

import (
	"os"

	"github.com/decisiveai/mdai-gateway/internal/valkey"
	"go.uber.org/zap"
)

type Config struct {
	HTTPPort     string
	OTelEndpoint string
	ValkeyCfg    valkey.Config
	OTelDisabled bool
}

func loadConfig(logger *zap.Logger) *Config {
	otelDisabledStr := os.Getenv(otelSdkDisabledEnvVar)
	otelEndpointStr := os.Getenv(otelExporterOtlpEndpointEnvVar)

	if otelDisabledStr == "" && otelEndpointStr == "" {
		logger.Warn("Warning: No OTLP endpoint is defined, but OTEL SDK is enabled.")
	}

	cfg := &Config{
		HTTPPort:     getEnvVariableWithDefault(httpPortEnvVarKey, defaultHTTPPort),
		OTelDisabled: otelDisabledStr == "true",
		OTelEndpoint: otelEndpointStr,
		ValkeyCfg: valkey.Config{
			InitAddress:            []string{getEnvVariableWithDefault(valkeyEndpointEnvVarKey, "mdai-valkey-primary.mdai.svc.cluster.local:6379")},
			Password:               getEnvVariableWithDefault(valkeyPasswordEnvVarKey, "abc"),
			InitialBackoffInterval: defaultInitialBackoff,
			MaxBackoffElapsedTime:  defaultMaxElapsedBackoff,
		},
	}
	return cfg
}

func getEnvVariableWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}
