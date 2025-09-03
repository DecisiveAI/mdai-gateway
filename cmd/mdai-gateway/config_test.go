package main

import (
	"strconv"
	"testing"
	"time"

	"github.com/decisiveai/mdai-data-core/helpers"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
)

func TestLoadConfig(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(t *testing.T)
		expected *Config
	}{
		{
			name:  "default",
			setup: func(t *testing.T) { t.Helper() },
			expected: &Config{
				HTTPPort:     "8081",
				OTelEndpoint: "",
				OTelDisabled: false,
			},
		},
		{
			name: "non-default values",
			setup: func(t *testing.T) {
				t.Helper()
				t.Setenv("OTEL_EXPORTER_OTLP_ENDPOINT", "otlp.mdai.svc.cluster.local:4317")
				t.Setenv("OTEL_SDK_DISABLED", "true")
				t.Setenv("HTTP_PORT", "8080")
				t.Setenv("VALKEY_ENDPOINT", "valkey-primary.mdai.svc.cluster.local:6379")
				t.Setenv("VALKEY_PASSWORD", "xyz")
				t.Setenv("VALKEY_AUDIT_STREAM_EXPIRY_MS", strconv.FormatInt((90*24*time.Hour).Milliseconds(), 10))
			},
			expected: &Config{
				HTTPPort:     "8080",
				OTelEndpoint: "otlp.mdai.svc.cluster.local:4317",
				OTelDisabled: true,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			tt.setup(t)
			cfg := loadConfig(zap.NewNop())
			assert.Equal(t, tt.expected, cfg)
		})
	}
}

func TestGetEnvVariableWithDefault(t *testing.T) {
	s := helpers.GetEnvVariableWithDefault("foo", "foo_default")
	assert.Equal(t, "foo_default", s)

	t.Setenv("bar", "not_bar_default")
	s = helpers.GetEnvVariableWithDefault("bar", "bar_default")
	assert.Equal(t, "not_bar_default", s)
}
