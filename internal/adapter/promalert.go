package adapter

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/mydecisive/mdai-data-core/eventing"
	"github.com/mydecisive/mdai-data-core/eventing/config"
	"github.com/prometheus/alertmanager/template"
	"go.uber.org/zap"
)

var ErrMissingFingerprint = errors.New("alert fingerprint is required")

const (
	hubName      = "hub_name"
	currentValue = "current_value"
	AlertName    = "alert_name"
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

func (w *PromAlertWrapper) ToMdaiEvents() ([]EventPerSubject, int, error) {
	skipped := 0
	alerts := w.Alerts // we don't need sorting within the same payload since it's deduplicated by fingerprint

	eventsPerSubject := make([]EventPerSubject, 0, len(alerts))
	for _, alert := range alerts {
		if alert.Fingerprint == "" {
			return nil, 0, fmt.Errorf("%w (name=%q status=%s)", ErrMissingFingerprint,
				alert.Annotations[AlertName], alert.Status)
		}
		changeTime := changeTime(alert)
		if isNewer, lastTime := w.deduper.UpdateIfNewer(alert.Fingerprint, changeTime); !isNewer {
			skipped++
			w.Logger.Info(
				"Skipping stale alert",
				zap.String("alert_name", alert.Annotations[AlertName]),
				zap.Time("last_update", lastTime),
				zap.Time("this_change", changeTime),
			)
			continue
		}
		event, err := w.toMdaiEvent(alert)
		if err != nil {
			return nil, 0, err
		}

		subj := subjectFromAlert(alert, event.HubName)
		w.Logger.Debug("subject for alert", zap.String("alert_name", alert.Annotations[AlertName]), zap.String("subject", subj.String()))

		eventPerSubject := EventPerSubject{
			Event:   event,
			Subject: subj,
		}

		eventsPerSubject = append(eventsPerSubject, eventPerSubject)
	}

	return eventsPerSubject, skipped, nil
}

// subjectFromAlert creates a subject from an alert. Prefix has to be added later at eventing package.
func subjectFromAlert(alert template.Alert, hubName string) eventing.MdaiEventSubject {
	return eventing.MdaiEventSubject{
		Type: eventing.AlertEventType,
		Path: hubName + "." + config.SafeToken(alert.Fingerprint),
	}
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
		Name:          alert.Annotations[AlertName] + "." + alert.Status,
		Source:        eventing.PrometheusAlertsEventSource,
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
