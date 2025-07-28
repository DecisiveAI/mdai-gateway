package main

import (
	"io"
	"strconv"
	"testing"
	"time"

	"github.com/decisiveai/mdai-gateway/internal/valkey"
	"github.com/stretchr/testify/assert"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
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
				ValkeyCfg: valkey.Config{
					Password:               "abc",
					InitAddress:            []string{"mdai-valkey-primary.mdai.svc.cluster.local:6379"},
					InitialBackoffInterval: 5 * time.Second,
					MaxBackoffElapsedTime:  3 * time.Minute,
					AuditStreamExpiration:  30 * 24 * time.Hour,
				},
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
				ValkeyCfg: valkey.Config{
					Password:               "xyz",
					InitAddress:            []string{"valkey-primary.mdai.svc.cluster.local:6379"},
					InitialBackoffInterval: 5 * time.Second,
					MaxBackoffElapsedTime:  3 * time.Minute,
					AuditStreamExpiration:  90 * 24 * time.Hour,
				},
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
	s := getEnvVariableWithDefault("foo", "foo_default")
	assert.Equal(t, "foo_default", s)

	t.Setenv("bar", "not_bar_default")
	s = getEnvVariableWithDefault("bar", "bar_default")
	assert.Equal(t, "not_bar_default", s)
}

type panicCore struct {
	zapcore.Core
}

func (c panicCore) Check(entry zapcore.Entry, ce *zapcore.CheckedEntry) *zapcore.CheckedEntry {
	if entry.Level == zapcore.FatalLevel {
		return ce.AddCore(entry, c)
	}
	return c.Core.Check(entry, ce)
}

func (c panicCore) Write(entry zapcore.Entry, fields []zapcore.Field) error {
	if entry.Level == zapcore.FatalLevel {
		panic("logger.Fatal called: " + entry.Message)
	}
	return c.Core.Write(entry, fields)
}

func newPanicLogger() *zap.Logger {
	core := panicCore{
		zapcore.NewCore(
			zapcore.NewJSONEncoder(zap.NewDevelopmentEncoderConfig()),
			zapcore.AddSync(io.Discard),
			zap.DebugLevel,
		),
	}
	return zap.New(core)
}

func TestGetValkeyAuditStreamExpiry(t *testing.T) {
	cases := []struct {
		name          string
		envValue      string
		expectPanic   bool
		expectedValue time.Duration
	}{
		{
			name:          "default value when env not set",
			envValue:      "",
			expectedValue: 30 * 24 * time.Hour,
		},
		{
			name:          "valid custom value",
			envValue:      strconv.FormatInt((90 * 24 * time.Hour).Milliseconds(), 10),
			expectedValue: 90 * 24 * time.Hour,
		},
		{
			name:        "invalid env value causes panic",
			envValue:    "panic",
			expectPanic: true,
		},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if tc.envValue != "" {
				t.Setenv("VALKEY_AUDIT_STREAM_EXPIRY_MS", tc.envValue)
			}

			if tc.expectPanic {
				assert.PanicsWithValue(t,
					"logger.Fatal called: Invalid Valkey stream expiry env var",
					func() {
						getValkeyAuditStreamExpiry(newPanicLogger())
					},
				)
				return
			}

			expiry := getValkeyAuditStreamExpiry(zap.NewNop())
			assert.Equal(t, tc.expectedValue, expiry)
		})
	}
}
