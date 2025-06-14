package types

import (
	"encoding/json"
	"log"
	"sort"

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

func CreateEventUuid() string {
	id := uuid.New()
	return id.String()
}

func AdaptPrometheusAlertToMdaiEvents(payload template.Data) []eventing.MdaiEvent {
	sort.Slice(payload.Alerts, func(i, j int) bool {
		return payload.Alerts[i].StartsAt.Before(payload.Alerts[j].StartsAt)
	})

	mdaiEvents := make([]eventing.MdaiEvent, 0)

	for _, alert := range payload.Alerts {
		annotations := alert.Annotations
		labels := alert.Labels
		status := alert.Status

		unMarshalledPayload := make(map[string]interface{})
		for key, value := range labels {
			unMarshalledPayload[key] = value
		}
		unMarshalledPayload["value"] = annotations[CurrentValue]
		unMarshalledPayload["status"] = status

		payloadBytes, err := json.Marshal(unMarshalledPayload)
		if err != nil {
			// TODO: Use our logger
			log.Fatal(err)
		}

		id := alert.Fingerprint
		if id == "" {
			id = CreateEventUuid()
		}

		mdaiEvent := eventing.MdaiEvent{
			Name:      annotations[AlertName] + "." + status,
			Source:    Prometheus,
			Id:        id,
			Timestamp: alert.StartsAt,
			HubName:   annotations[HubName],
			Payload:   string(payloadBytes),
		}

		// TODO: Log event created
		mdaiEvents = append(mdaiEvents, mdaiEvent)
	}

	return mdaiEvents
}
