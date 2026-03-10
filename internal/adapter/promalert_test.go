package adapter

import (
	"encoding/json"
	"testing"
	"time"

	"github.com/mydecisive/mdai-data-core/eventing"
	"github.com/prometheus/alertmanager/template"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
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

	deduper := NewDeduper() // shared across subtests is fine since fingerprints differ
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			input := template.Data{Alerts: tt.alerts}
			wrappedInput := NewPromAlertWrapper(input, zap.NewNop(), deduper)

			events, skipped, err := wrappedInput.ToMdaiEvents()
			require.NoError(t, err)
			require.Equal(t, 0, skipped)
			require.Len(t, events, len(tt.alerts))

			// Order check (when provided)
			if tt.expectOrder != nil {
				for i, expectedID := range tt.expectOrder {
					require.Equal(t, expectedID, events[i].Event.SourceID)
				}
				return
			}

			// Common field checks for single-alert cases
			e := events[0]
			require.Equal(t, "DiskUsageHigh.firing", e.Event.Name)
			require.Equal(t, "prod-cluster", e.Event.HubName)
			require.Equal(t, eventing.PrometheusAlertsEventSource, e.Event.Source)
			require.NotEmpty(t, e.Event.ID)
			require.NotEmpty(t, e.Event.CorrelationID)

			// SourceID expectations
			if tt.expectIDExact != "" {
				require.Equal(t, tt.expectIDExact, e.Event.SourceID)
			} else {
				// no fingerprint => we expect a non-empty fallback SourceID
				require.NotEmpty(t, e.Event.SourceID)
			}

			var payload struct {
				Labels      map[string]string `json:"labels"`
				Annotations map[string]string `json:"annotations"`
				Status      string            `json:"status"`
				Value       string            `json:"value"`
			}
			err = json.Unmarshal([]byte(e.Event.Payload), &payload)
			require.NoError(t, err)
			require.Equal(t, "92%", payload.Value)
			require.Equal(t, "firing", payload.Status)
			require.Equal(t, "critical", payload.Labels["severity"])

			// Verify each alert maps to some event (fingerprint-aware)
			for idx, alert := range tt.alerts {
				if alert.Fingerprint == "" {
					// no exact match possible; ensure at least one event has non-empty SourceID
					require.NotEmpty(t, events[idx].Event.SourceID, "event %d should carry a generated SourceID", idx)
					continue
				}
				found := false
				for _, event := range events {
					found = found || (alert.Fingerprint == event.Event.SourceID)
				}
				require.True(t, found, "alert fingerprint for event index %d was not found in any events", idx)
			}
		})
	}
}

// Verifies that an alert without a fingerprint is rejected with ErrMissingFingerprint.
func TestPrometheusAlertWithoutFingerprint(t *testing.T) {
	now := time.Now()

	alert := template.Alert{
		Annotations: template.KV{
			"alert_name":    "DiskUsageHigh",
			"hub_name":      "prod-cluster",
			"current_value": "92%",
		},
		Labels:   template.KV{"severity": "critical"},
		Status:   "firing",
		StartsAt: now.Add(-1 * time.Minute),
		// Fingerprint intentionally omitted
	}

	input := template.Data{Alerts: []template.Alert{alert}}
	deduper := NewDeduper() // shared/global in real server wiring
	wrapped := NewPromAlertWrapper(input, zap.NewNop(), deduper)

	events, skipped, err := wrapped.ToMdaiEvents()
	require.ErrorIs(t, err, ErrMissingFingerprint)
	require.Empty(t, events)
	require.Equal(t, 0, skipped)
}

func TestLatePrometheusAlert(t *testing.T) {
	now := time.Now()

	alerts := []template.Alert{
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
		{
			Annotations: template.KV{
				"alert_name":    "DiskUsageHigh",
				"hub_name":      "prod-cluster",
				"current_value": "92%",
			},
			Labels:      template.KV{"severity": "critical"},
			Status:      "firing",
			StartsAt:    now.Add(-2 * time.Minute),
			Fingerprint: "abc123",
		},
	}

	input := template.Data{Alerts: alerts}
	deduper := NewDeduper() // shared/global in real server wiring
	wrapped := NewPromAlertWrapper(input, zap.NewNop(), deduper)

	_, skipped, err := wrapped.ToMdaiEvents()
	require.NoError(t, err)
	require.Equal(t, 1, skipped)
}
