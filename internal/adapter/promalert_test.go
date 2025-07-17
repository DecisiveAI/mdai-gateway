package adapter

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/require"
)

func TestPrometheusAlertToMdaiEvents(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name          string
		expectIDExact string
		alerts        []template.Alert
		expectOrder   []string
	}{
		{
			name: "with fingerprint",
			alerts: []template.Alert{
				{
					Annotations: template.KV{
						"alert_name":    "DiskUsageHigh",
						"hub_name":      "prod-cluster",
						"current_value": "92%",
					},
					Labels:      template.KV{"severity": "critical"},
					Status:      "firing",
					StartsAt:    now.Add(-1 * time.Minute),
					Fingerprint: "abc123",
				},
			},
			expectIDExact: "abc123",
		},
		{
			name: "without fingerprint",
			alerts: []template.Alert{
				{
					Annotations: template.KV{
						"alert_name":    "DiskUsageHigh",
						"hub_name":      "prod-cluster",
						"current_value": "92%",
					},
					Labels:   template.KV{"severity": "critical"},
					Status:   "firing",
					StartsAt: now.Add(-1 * time.Minute),
				},
			},
		},
		{
			name: "sorts by StartsAt",
			alerts: []template.Alert{
				{
					Annotations: template.KV{
						"alert_name":    "OlderAlert",
						"hub_name":      "prod-cluster",
						"current_value": "1",
					},
					Labels:      template.KV{"severity": "low"},
					Status:      "firing",
					StartsAt:    now.Add(-2 * time.Minute),
					Fingerprint: "id1",
				},
				{
					Annotations: template.KV{
						"alert_name":    "NewerAlert",
						"hub_name":      "prod-cluster",
						"current_value": "2",
					},
					Labels:      template.KV{"severity": "critical"},
					Status:      "firing",
					StartsAt:    now.Add(-1 * time.Minute),
					Fingerprint: "id2",
				},
			},
			expectOrder: []string{"id1", "id2"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := template.Data{Alerts: tt.alerts}
			wrappedInput := NewPromAlertWrapper(input)
			events, err := wrappedInput.ToMdaiEvents()
			require.NoError(t, err)
			require.Len(t, events, len(tt.alerts))

			if tt.expectOrder != nil {
				for i, expectedID := range tt.expectOrder {
					require.Equal(t, expectedID, events[i].SourceId)
				}

				return
			}

			e := events[0]
			require.Equal(t, "DiskUsageHigh.firing", e.Name)
			require.Equal(t, "prod-cluster", e.HubName)
			require.Equal(t, Prometheus, e.Source)

			require.NotEmpty(t, e.Id)
			require.NotEmpty(t, e.CorrelationId)

			for idx, alert := range tt.alerts {
				found := false
				for _, event := range events {
					found = alert.Fingerprint == event.SourceId
				}
				require.True(t, found, "alert fingerprint for event index %d was not found in any events", idx)
			}

			var payload map[string]string

			err = json.Unmarshal([]byte(e.Payload), &payload)
			require.NoError(t, err)
			require.Equal(t, "92%", payload["value"])
			require.Equal(t, "firing", payload["status"])
			require.Equal(t, "critical", payload["severity"])
		})
	}
}
