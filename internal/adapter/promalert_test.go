package adapter

import (
	"encoding/json"
	"strconv"
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

// Adaptation must not commit dedupe state, or a failed publish would swallow
// Alertmanager's retry (same fingerprint, same change time) as stale.
func TestToMdaiEventsDoesNotCommitDedupeState(t *testing.T) {
	now := time.Now()

	alert := template.Alert{
		Annotations: template.KV{
			"alert_name":    "DiskUsageHigh",
			"hub_name":      "prod-cluster",
			"current_value": "92%",
		},
		Labels:      template.KV{"severity": "critical"},
		Status:      "firing",
		StartsAt:    now.Add(-1 * time.Minute),
		Fingerprint: "abc123",
	}
	input := template.Data{Alerts: []template.Alert{alert}}
	deduper := NewDeduper()
	wrapper := NewPromAlertWrapper(input, zap.NewNop(), deduper)

	events, skipped, err := wrapper.ToMdaiEvents()
	require.NoError(t, err)
	require.Len(t, events, 1)
	require.Equal(t, 0, skipped)

	_, committed := deduper.PeekLast(dedupeKey("prod-cluster", "abc123"))
	require.False(t, committed, "adaptation must not mark the alert as seen before publish")

	// Simulate the retry after a failed publish: the same payload must adapt again.
	events, skipped, err = NewPromAlertWrapper(input, zap.NewNop(), deduper).ToMdaiEvents()
	require.NoError(t, err)
	require.Len(t, events, 1, "retry of an unpublished alert must not be deduplicated")
	require.Equal(t, 0, skipped)

	// Once committed (successful publish), the same payload is skipped as stale.
	wrapper.CommitPublished(events[0])
	events, skipped, err = NewPromAlertWrapper(input, zap.NewNop(), deduper).ToMdaiEvents()
	require.NoError(t, err)
	require.Empty(t, events)
	require.Equal(t, 1, skipped)
}

func TestCommitPublishedUsesPeekedChangeTime(t *testing.T) {
	alert := template.Alert{
		Annotations: template.KV{
			"alert_name": "DiskUsageHigh",
			"hub_name":   "prod-cluster",
		},
		Labels:      template.KV{"severity": "critical"},
		Status:      "firing",
		Fingerprint: "abc123",
		// StartsAt deliberately zero: changeTime peeks as the zero time.
	}
	deduper := NewDeduper()
	wrapper := NewPromAlertWrapper(template.Data{Alerts: []template.Alert{alert}}, zap.NewNop(), deduper)

	events, _, err := wrapper.ToMdaiEvents()
	require.NoError(t, err)
	require.Len(t, events, 1)
	wrapper.CommitPublished(events[0])

	committed, seen := deduper.PeekLast(dedupeKey("prod-cluster", "abc123"))
	require.True(t, seen)
	require.True(t, committed.IsZero(), "commit must store the peeked change time, got %v", committed)

	// A real delivery whose StartsAt predates the commit instant must still pass.
	alert.StartsAt = time.Now().Add(-1 * time.Minute)
	events, skipped, err := NewPromAlertWrapper(template.Data{Alerts: []template.Alert{alert}}, zap.NewNop(), deduper).ToMdaiEvents()
	require.NoError(t, err)
	require.Len(t, events, 1, "a legitimate later delivery must not be skipped as stale")
	require.Equal(t, 0, skipped)
}

// Fingerprints hash only the label set, and hub_name is an annotation — two hubs
// can produce the same fingerprint. Dedup identity must include the hub.
func TestToMdaiEventsDeduplicatesPerHub(t *testing.T) {
	now := time.Now()
	hubAlert := func(hub string) template.Alert {
		return template.Alert{
			Annotations: template.KV{
				"alert_name":    "DiskUsageHigh",
				"hub_name":      hub,
				"current_value": "92%",
			},
			Labels:      template.KV{"severity": "critical"},
			Status:      "firing",
			StartsAt:    now.Add(-1 * time.Minute),
			Fingerprint: "abc123",
		}
	}
	payload := func(hub string) template.Data {
		return template.Data{Alerts: []template.Alert{hubAlert(hub)}}
	}
	deduper := NewDeduper()

	// Hub A's alert publishes and commits.
	wrapperA := NewPromAlertWrapper(payload("hub-a"), zap.NewNop(), deduper)
	eventsA, _, err := wrapperA.ToMdaiEvents()
	require.NoError(t, err)
	require.Len(t, eventsA, 1)
	wrapperA.CommitPublished(eventsA[0])

	// Hub B shares fingerprint and change time but is a different alert.
	eventsB, skipped, err := NewPromAlertWrapper(payload("hub-b"), zap.NewNop(), deduper).ToMdaiEvents()
	require.NoError(t, err)
	require.Len(t, eventsB, 1, "an alert from another hub must not be deduplicated")
	require.Equal(t, 0, skipped)
	require.NotEqual(t, eventsA[0].Event.ID, eventsB[0].Event.ID,
		"event IDs must differ across hubs or JetStream drops one as a broker-side duplicate")

	// Hub A's re-delivery is still recognized as stale.
	_, skipped, err = NewPromAlertWrapper(payload("hub-a"), zap.NewNop(), deduper).ToMdaiEvents()
	require.NoError(t, err)
	require.Equal(t, 1, skipped)
}

// The event ID doubles as the Nats-Msg-Id, so identical deliveries of the same
// alert state must produce identical IDs for JetStream's duplicate window to match.
func TestToMdaiEventsDeterministicEventID(t *testing.T) {
	now := time.Now()

	alert := template.Alert{
		Annotations: template.KV{
			"alert_name":    "DiskUsageHigh",
			"hub_name":      "prod-cluster",
			"current_value": "92%",
		},
		Labels:      template.KV{"severity": "critical"},
		Status:      "firing",
		StartsAt:    now.Add(-1 * time.Minute),
		EndsAt:      now,
		Fingerprint: "abc123",
	}
	input := template.Data{Alerts: []template.Alert{alert}}

	first, _, err := NewPromAlertWrapper(input, zap.NewNop(), NewDeduper()).ToMdaiEvents()
	require.NoError(t, err)
	second, _, err := NewPromAlertWrapper(input, zap.NewNop(), NewDeduper()).ToMdaiEvents()
	require.NoError(t, err)

	wantID := "prod-cluster/abc123-" + strconv.FormatInt(alert.StartsAt.UnixNano(), 10)
	require.Equal(t, wantID, first[0].Event.ID)
	require.Equal(t, wantID, second[0].Event.ID, "re-delivery of the same alert state must reuse the ID")

	// A resolved delivery is a new state (EndsAt) and must not collide.
	resolved := alert
	resolved.Status = "resolved"
	resolvedEvents, _, err := NewPromAlertWrapper(template.Data{Alerts: []template.Alert{resolved}}, zap.NewNop(), NewDeduper()).ToMdaiEvents()
	require.NoError(t, err)
	require.Equal(t, "prod-cluster/abc123-"+strconv.FormatInt(alert.EndsAt.UnixNano(), 10), resolvedEvents[0].Event.ID)
	require.NotEqual(t, first[0].Event.ID, resolvedEvents[0].Event.ID)
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
