package prometheusalert

import (
	"testing"
	"time"

	alertmanager "github.com/prometheus/alertmanager/template"
	"go.uber.org/zap"
)

func BenchmarkToMdaiEvent(b *testing.B) {
	payload := loadSamplePrometheusAlertData()
	logger := zap.NewNop() // minimal logger to avoid log output overhead
	b.ReportAllocs()

	b.ResetTimer() // exclude setup time
	for range b.N {
		_ = payload.ToMdaiEvent(logger)
	}
}

func loadSamplePrometheusAlertData() PrometheusAlertData {
	return PrometheusAlertData{
		Data: alertmanager.Data{
			Receiver: "mdai",
			Alerts: []alertmanager.Alert{
				{
					Status: "firing",
					Labels: map[string]string{
						"alertname": "NoisyServiceAlert",
					},
					Annotations: map[string]string{
						"description": "Something is broken",
						"hubName":     "mdai-hub",
					},
					StartsAt: time.Now(),
				},
			},
		},
	}
}
