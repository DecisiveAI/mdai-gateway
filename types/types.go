package types

import (
	mdaiv1 "github.com/decisiveai/mdai-operator/api/v1"
)

type Config struct {
	Evaluations []mdaiv1.Evaluation `json:"evaluations" yaml:"evaluations"`
	Variables   []mdaiv1.Variable   `json:"variables" yaml:"variables"`
}
