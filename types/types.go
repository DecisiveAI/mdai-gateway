package types

import (
	"encoding/json"
	"github.com/prometheus/alertmanager/template"
	"log"
	"os/exec"
	"sort"
	"time"

	mdaiv1 "github.com/decisiveai/mdai-operator/api/v1"
)

type AlertManagerPayload struct {
	Receiver          string            `json:"receiver"`
	Status            string            `json:"status"`
	Alerts            []Alert           `json:"alerts"`
	GroupLabels       map[string]string `json:"groupLabels"`
	CommonLabels      map[string]string `json:"commonLabels"`
	CommonAnnotations map[string]string `json:"commonAnnotations"`
	ExternalURL       string            `json:"externalURL"`
}

// for now ignore the unused warning from linting
//
//nolint:golint,unused
func (payload *AlertManagerPayload) isFiring() bool {
	return payload.Status == "firing"
}

type Alert struct {
	Status       string            `json:"status"`
	Labels       map[string]string `json:"labels"`
	Annotations  map[string]string `json:"annotations"`
	StartsAt     time.Time         `json:"startsAt"`
	EndsAt       time.Time         `json:"endsAt"`
	GeneratorURL string            `json:"generatorURL"`
	Fingerprint  string            `json:"fingerprint"`
}

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
		unMarshalledPayload["value"] = annotations["current_value"]
		unMarshalledPayload["hubName"] = annotations["hub_name"]

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
			Name:      annotations["alert_name"] + "." + status,
			Source:    "prometheus",
			Id:        Id,
			Timestamp: alert.StartsAt.String(),
			Payload:   string(payloadBytes),
		}

		// TODO: Log event created
		mdaiEvents = append(mdaiEvents, mdaiEvent)
	}

	return mdaiEvents
}
