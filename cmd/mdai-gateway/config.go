package main

import (
	"os"
	"strconv"
	"time"

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
			AuditStreamExpiration:  getValkeyAuditStreamExpiry(logger),
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

const (
	valkeyAuditStreamExpiryMSEnvVarKey = "VALKEY_AUDIT_STREAM_EXPIRY_MS"
	valkeyAuditStreamExpiry            = 30 * 24 * time.Hour
)

func getValkeyAuditStreamExpiry(logger *zap.Logger) time.Duration {
	expiryStr := os.Getenv(valkeyAuditStreamExpiryMSEnvVarKey)
	if expiryStr == "" {
		logger.Info("Using default stream expiry", zap.Duration("expiry", valkeyAuditStreamExpiry))
		return valkeyAuditStreamExpiry
	}

	expiryMs, err := strconv.Atoi(expiryStr)
	if err != nil {
		logger.Fatal("Invalid Valkey stream expiry env var", zap.String("value", expiryStr), zap.Error(err))
	}

	expiry := time.Duration(expiryMs) * time.Millisecond
	logger.Info("Using custom Valkey stream expiry", zap.Duration("expiry", expiry))

	return expiry
}
