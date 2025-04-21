package types

import (
	mdaiv1 "github.com/decisiveai/mdai-operator/api/v1"
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
