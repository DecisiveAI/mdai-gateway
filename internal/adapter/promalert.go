package adapter

import (
	"encoding/json"
	"fmt"
	"sort"

	"github.com/decisiveai/mdai-event-hub/eventing"
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

	event := eventing.MdaiEvent{
		Name:      annotations[AlertName] + "." + alert.Status,
		Source:    Prometheus,
		Id:        alert.Fingerprint,
		Timestamp: alert.StartsAt,
		HubName:   annotations[HubName],
		Payload:   string(payloadJSON),
	}
	event.ApplyDefaults()

	return event, nil
}
