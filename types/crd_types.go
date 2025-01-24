package types

import (
	prometheusv1 "github.com/prometheus-operator/prometheus-operator/pkg/apis/monitoring/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
)

type (
	ActionName          string
	EvaluationName      string
	VariableStorageType string
	VariableType        string
)

type Evaluation struct {
	Name           EvaluationName                   `json:"name" yaml:"name"`
	Type           string                           `json:"type" yaml:"type"`
	Expr           string                           `json:"expr" yaml:"expr"`
	For            *prometheusv1.Duration           `json:"for,omitempty" yaml:"for,omitempty"`
	KeepFiringFor  *prometheusv1.NonEmptyDuration   `json:"keep_firing_for,omitempty" yaml:"keep_firing_for,omitempty"`
	Severity       string                           `json:"severity" yaml:"severity"`
	RelevantLabels *[]string                        `json:"relevantLabels" yaml:"relevantLabels"`
	Resolve        *Action                          `json:"resolve" yaml:"resolve"`
	Status         *PrometheusAlertEvaluationStatus `json:"status" yaml:"status"`
	Interval       *metav1.Duration                 `json:"interval,omitempty" yaml:"interval,omitempty"`
}

type PrometheusAlertEvaluationStatus struct {
	Firing   *Action `json:"firing" yaml:"firing"`
	Resolved *Action `json:"resolved" yaml:"resolved"`
}

type VariableUpdate struct {
	VariableRef string `json:"variableRef" yaml:"variableRef"`
	Operation   string `json:"operation" yaml:"operation"`
}

type Action struct {
	VariableUpdate *VariableUpdate `json:"variableUpdate" yaml:"variableUpdate"`
}

type VariableWith struct {
	ExportedVariableName string               `json:"exportedVariableName,omitempty" yaml:"exportedVariableName,omitempty"`
	Transformer          *VariableTransformer `json:"transformer,omitempty" yaml:"transformer,omitempty"`
}

type JoinFunction struct {
	Delimiter string `json:"delimiter" yaml:"delimiter"`
}

type VariableTransformer struct {
	Join *JoinFunction `json:"join,omitempty" yaml:"join,omitempty"`
}

type Variable struct {
	StorageKey   string               `json:"storageKey" yaml:"storageKey"`
	Type         VariableType         `json:"type" yaml:"type"`
	StorageType  *VariableStorageType `json:"storageType" yaml:"storageType"`
	DefaultValue *string              `json:"defaultValue,omitempty" yaml:"defaultValue,omitempty"`
	With         *[]VariableWith      `json:"with,omitempty" yaml:"with,omitempty"`
}
