package types

import (
	"encoding/json"
	mdaiv1 "github.com/decisiveai/mdai-operator/api/v1"
	"github.com/prometheus/alertmanager/template"
	"log"
	"os/exec"
	"sort"
)

const (
	HubName      = "hub_name"
	CurrentValue = "current_value"
	AlertName    = "alert_name"
	Prometheus   = "prometheus"
)

type Config struct {
	Evaluations []mdaiv1.Evaluation `json:"evaluations" yaml:"evaluations"`
	Variables   []mdaiv1.Variable   `json:"variables" yaml:"variables"`
}

type MdaiEvent struct {
	Name      string `json:"name"`
	Source    string `json:"source"`
	Id        string `json:"id,omitempty"`
	Timestamp string `json:"timestamp"`
	Payload   string `json:"payload,omitempty"`
}

func CreateEventUuid() (string, error) {
	uuidBytes, err := exec.Command("uuidgen").Output()
	if err != nil {
		// TODO: Use our logger
		log.Fatal(err)
		return "", err
	}

	return string(uuidBytes), nil
}

func AdaptPrometheusAlertToMdaiEvents(payload template.Data) []MdaiEvent {
	sort.Slice(payload.Alerts, func(i, j int) bool {
		return payload.Alerts[i].StartsAt.Before(payload.Alerts[j].StartsAt)
	})

	mdaiEvents := make([]MdaiEvent, 0)

	for _, alert := range payload.Alerts {
		annotations := alert.Annotations
		labels := alert.Labels
		status := alert.Status

		unMarshalledPayload := make(map[string]interface{})
		for key, value := range labels {
			unMarshalledPayload[key] = value
		}
		unMarshalledPayload["value"] = annotations[CurrentValue]
		unMarshalledPayload["hubName"] = annotations[HubName]

		payloadBytes, err := json.Marshal(unMarshalledPayload)
		if err != nil {
			// TODO: Use our logger
			log.Fatal(err)
		}

		var Id string
		if alert.Fingerprint != "" {
			Id = alert.Fingerprint
		} else {
			uuid, err := CreateEventUuid()
			if err == nil {
				Id = uuid
			}
		}

		mdaiEvent := MdaiEvent{
			Name:      annotations[AlertName] + "." + status,
			Source:    Prometheus,
			Id:        Id,
			Timestamp: alert.StartsAt.String(),
			Payload:   string(payloadBytes),
		}

		// TODO: Log event created
		mdaiEvents = append(mdaiEvents, mdaiEvent)
	}

	return mdaiEvents
}
