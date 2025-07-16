package adapter

import (
	"encoding/json"
	"fmt"
	"sort"
	"time"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/google/uuid"
	"github.com/prometheus/alertmanager/template"
)

const (
	HubName      = "hub_name"
	CurrentValue = "current_value"
	AlertName    = "alert_name"
	Prometheus   = "prometheus"
)

type PromAlertWrapper struct {
	*template.Data
}

var _ EventAdapter = (*PromAlertWrapper)(nil)

func NewPromAlertWrapper(v template.Data) *PromAlertWrapper {
	return &PromAlertWrapper{Data: &v}
}

func (w *PromAlertWrapper) ToMdaiEvents() ([]eventing.MdaiEvent, error) {
	sort.Slice(w.Alerts, func(i, j int) bool {
		return w.Alerts[i].StartsAt.Before(w.Data.Alerts[j].StartsAt)
	})

	mdaiEvents := make([]eventing.MdaiEvent, 0, len(w.Alerts))
	for _, alert := range w.Alerts {
		event, err := toMdaiEvent(alert)
		if err != nil {
			return nil, err
		}

		mdaiEvents = append(mdaiEvents, event)
	}

	return mdaiEvents, nil
}

func toMdaiEvent(alert template.Alert) (eventing.MdaiEvent, error) {
	annotations := alert.Annotations

	unmarshalled := map[string]any{}
	for k, v := range alert.Labels {
		unmarshalled[k] = v
	}

	unmarshalled["value"] = annotations[CurrentValue]
	unmarshalled["status"] = alert.Status

	payloadJSON, err := json.Marshal(unmarshalled)
	if err != nil {
		return eventing.MdaiEvent{}, fmt.Errorf("marshal payload: %w", err)
	}

	id := uuid.New().String()
	correlationIdCore := alert.Fingerprint
	if correlationIdCore == "" {
		correlationIdCore = uuid.New().String()
	}
	correlationID := fmt.Sprintf("%d-%s", time.Now().UnixMilli(), correlationIdCore)

	event := eventing.MdaiEvent{
		Id:            id,
		Name:          annotations[AlertName] + "." + alert.Status,
		Source:        Prometheus,
		SourceId:      alert.Fingerprint,
		Timestamp:     alert.StartsAt,
		HubName:       annotations[HubName],
		Payload:       string(payloadJSON),
		CorrelationId: correlationID,
	}
	event.ApplyDefaults()

	return event, nil
}
