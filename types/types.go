package types

import (
	"time"

	mdaiv1 "github.com/DecisiveAI/mdai-operator/api/v1"
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

type MdaiHubEvent struct {
	Type      string `json:"type"`
	Operation string `json:"operation"`
	Variable  string `json:"variable"`
	Value     string `json:"value"`
}
