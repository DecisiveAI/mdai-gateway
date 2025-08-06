package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/decisiveai/mdai-event-hub/pkg/eventing"
	"github.com/google/uuid"
	"github.com/prometheus/alertmanager/template"
	"go.uber.org/zap"
)

var ErrMissingFingerprint = errors.New("alert fingerprint is required")

const (
	hubName      = "hub_name"
	currentValue = "current_value"
	AlertName    = "alert_name"
	Prometheus   = "prometheus"
)

type PromAlertWrapper struct {
	*template.Data

	Logger  *zap.Logger
	deduper *Deduper
}

var _ EventAdapter = (*PromAlertWrapper)(nil)

func NewPromAlertWrapper(v template.Data, l *zap.Logger, d *Deduper) *PromAlertWrapper {
	return &PromAlertWrapper{Data: &v, Logger: l, deduper: d}
}

func (w *PromAlertWrapper) ToMdaiEvents() ([]eventing.MdaiEvent, int, error) {
	skipped := 0
	alerts := w.Alerts // we don't need sorting within the same payload since it's deduplicated by fingerprint

	mdaiEvents := make([]eventing.MdaiEvent, 0, len(alerts))
	for _, alert := range alerts {
		if alert.Fingerprint == "" {
			return nil, 0, fmt.Errorf("%w (name=%q status=%s)", ErrMissingFingerprint,
				alert.Annotations[AlertName], alert.Status)
		}
		if !w.deduper.IsNewer(alert.Fingerprint, changeTime(alert)) {
			skipped++
			w.Logger.Info(
				"Skipping alert",
				zap.String("alert_name", alert.Annotations[AlertName]),
				zap.Time("last_update", w.deduper.last[alert.Fingerprint]),
			)
			continue
		}
		event, err := w.toMdaiEvent(alert)
		if err != nil {
			return nil, 0, err
		}

		mdaiEvents = append(mdaiEvents, event)
	}

	return mdaiEvents, skipped, nil
}

func (w *PromAlertWrapper) toMdaiEvent(alert template.Alert) (eventing.MdaiEvent, error) {
	annotations := alert.Annotations

	payload := struct {
		Labels      map[string]string `json:"labels"`
		Annotations map[string]string `json:"annotations,omitempty"`
		Status      string            `json:"status"`
		Value       string            `json:"value,omitempty"`
	}{
		Labels:      alert.Labels,
		Annotations: alert.Annotations,
		Status:      alert.Status,
		Value:       alert.Annotations[currentValue],
	}

	payloadJSON, err := json.Marshal(payload)
	if err != nil {
		return eventing.MdaiEvent{}, fmt.Errorf("marshal payload: %w", err)
	}

	correlationIDCore := alert.Fingerprint
	if correlationIDCore == "" {
		correlationIDCore = uuid.New().String()
	}
	correlationID := fmt.Sprintf("%d-%s", time.Now().UnixMilli(), correlationIDCore)

	event := eventing.MdaiEvent{
		Name:          annotations[AlertName] + "." + alert.Status,
		Source:        Prometheus,
		SourceID:      alert.Fingerprint,
		Timestamp:     changeTime(alert),
		HubName:       annotations[hubName],
		Payload:       string(payloadJSON),
		CorrelationID: correlationID,
	}
	event.ApplyDefaults()

	if err := event.Validate(); err != nil {
		w.Logger.Error("Failed to validate MdaiEvent", zap.Error(err), zap.Inline(&event))
		return eventing.MdaiEvent{}, err
	}

	return event, nil
}

// changeTime returns the time when the alert status changed (resolved or not).
// If the status is not resolved, the change time is the start time. Otherwise, it's the end time.
func changeTime(a template.Alert) time.Time {
	if strings.EqualFold(a.Status, "resolved") {
		return a.EndsAt
	}
	return a.StartsAt
}
