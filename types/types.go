package types

import (
	mdaiv1 "github.com/DecisiveAI/mdai-operator/api/v1"
	"iter"
	"time"
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
	HubName    string `json:"hubName"`    //name of hub event was triggered
	Name       string `json:"name"`       //name of event to connect action
	Variable   string `json:"variable"`   //variable triggering event
	Type       string `json:"type"`       //triggered event
	MetricName string `json:"metricName"` //expr delta & metric measured by observer
	Expression string `json:"expression"` //expr used to trigger event
	Value      string `json:"value"`      //value of metric when event triggered
	Status     string `json:"status"`     //status of event (active, updated)
}

func (hubEvent MdaiHubEvent) ToSequence() iter.Seq2[string, string] {
	return func(yield func(K string, V string) bool) {
		fields := []struct {
			key   string
			value string
		}{
			{"timestamp", time.Now().UTC().Format(time.RFC3339)},
			{"hubName", hubEvent.HubName},
			{"name", hubEvent.Name},
			{"variable", hubEvent.Variable},
			{"type", hubEvent.Type},
			{"metricName", hubEvent.MetricName},
			{"expression", hubEvent.Expression},
			{"value", hubEvent.Value},
			{"status", hubEvent.Status},
		}
		for _, field := range fields {
			if field.value == "" {
				continue
			}
			if !yield(field.key, field.value) {
				return
			}
		}
	}
}

type MdaiHubAction struct {
	HubName         string `json:"hubName"`         //name of hub action was triggered
	EventName       string `json:"eventName"`       //name of event that caused action
	Type            string `json:"type"`            //type of action (variable_update, collector_restart)
	Operation       string `json:"operation"`       //operation to perform (add_element, remove_element)
	Target          string `json:"target"`          //target of action (ex. variable/mdaihub-sample/service_list)
	Variable        string `json:"variable"`        //variable affected by action
	StoredVariables string `json:"storedVariables"` //used with collector restart action to show stored variables
}

func (hubAction MdaiHubAction) ToSequence() iter.Seq2[string, string] {
	return func(yield func(K string, V string) bool) {
		fields := []struct {
			key   string
			value string
		}{
			{"timestamp", time.Now().UTC().Format(time.RFC3339)},
			{"hubName", hubAction.HubName},
			{"eventName", hubAction.EventName},
			{"type", hubAction.Type},
			{"operation", hubAction.Operation},
			{"target", hubAction.Target},
			{"variable", hubAction.Variable},
		}

		for _, field := range fields {
			if field.value == "" {
				continue
			}
			if !yield(field.key, field.value) {
				return
			}
		}
	}
}
