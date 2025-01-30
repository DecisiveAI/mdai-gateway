package main

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"

	"github.com/decisiveai/event-handler-webservice/types"
	"github.com/valkey-io/valkey-go"
)

const (
	valkeyEndpointEnvVarKey = "VALKEY_ENDPOINT"
	valkeyPasswordEnvVarKey = "VALKEY_PASSWORD"

	firingStatus   = "firing"
	resolvedStatus = "resolved"

	actionContextAnnotationsKey  = "action_context"
	relevantLabelsAnnotationsKey = "relevant_labels"
	HubName                     = "hub_name"

	AddElement    = "mdai/add_element"
	RemoveElement = "mdai/remove_element"
	ReplaceValue  = "mdai/replace_value"

	VariableKeyPrefix = "variable/"
)

func main() {
	ctx := context.Background()

	valkeyClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{getEnvVariableWithDefault(valkeyEndpointEnvVarKey, "mdai-valkey-primary.mdai.svc.cluster.local:6379")},
		Password:    getEnvVariableWithDefault(valkeyPasswordEnvVarKey, ""),
	})

	if err != nil {
		log.Fatalf("failed to get valkey client: %v", err)
	}
	http.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))

	log.Println("Starting server on :8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func getEnvVariableWithDefault(key, defaultValue string) string {
	if value, exists := os.LookupEnv(key); exists {
		return value
	}
	return defaultValue
}

func handleAlertsPost(ctx context.Context, valkeyClient valkey.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to read body: %v", err)
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		log.Printf("payload: %v", string(body))

		var payload types.AlertManagerPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("invalid json: %v", body)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		for _, alert := range payload.Alerts {
			hubName := alert.Annotations[HubName]
			if hubName == "" {
				log.Printf("Skipping alert because no hub_name found in alert annotations, payload: %v", alert)
				continue
			}

			actionContextJSON := alert.Annotations[actionContextAnnotationsKey]
			if actionContextJSON == "" {
				log.Printf("Skipping alert because no action_context found in alert annotations, payload: %v", alert)
				continue
			}
			relevantLabelsJSON := alert.Annotations[relevantLabelsAnnotationsKey]
			if relevantLabelsJSON == "" {
				log.Printf("Skipping alert because no relevant_labels found in alert annotations, payload:  %v", alert)
				continue
			}

			var actionContext *types.PrometheusAlertEvaluationStatus
			if err := json.Unmarshal([]byte(actionContextJSON), &actionContext); err != nil {
				log.Printf("Skipping alert because could not unmarshal action_context for alert, payload: %v", alert)
				continue
			}
			relevantLabels := make([]string, 0)
			if err := json.Unmarshal([]byte(relevantLabelsJSON), &relevantLabels); err != nil {
				log.Printf("Skipping alert because could not unmarshal relevant_labels for alert, payload: %v", alert)
				continue
			}

			var variableUpdate *types.VariableUpdate
			switch alert.Status {
			case firingStatus:
				variableUpdate = actionContext.Firing.VariableUpdate
			case resolvedStatus:
				variableUpdate = actionContext.Resolved.VariableUpdate
			}

			valkeyKey := VariableKeyPrefix + hubName + "/" + variableUpdate.VariableRef
			switch variableUpdate.Operation {
			case AddElement:
				for _, element := range relevantLabels {
					log.Printf("adding element for %s", valkeyKey)
					if result := valkeyClient.Do(ctx, valkeyClient.B().Sadd().Key(valkeyKey).Member(alert.Labels[element]).Build()); result.Error() != nil {
						log.Printf("valkey error: %v", result.Error())
						http.Error(w, "valkey error: "+result.Error().Error(), http.StatusInternalServerError)
					}
				}
			case RemoveElement:
				for _, element := range relevantLabels {
					log.Printf("removing element for %s", valkeyKey)
					if result := valkeyClient.Do(ctx, valkeyClient.B().Srem().Key(valkeyKey).Member(alert.Labels[element]).Build()); result.Error() != nil {
						log.Printf("valkey error: %v", result.Error())
						http.Error(w, "valkey error: "+result.Error().Error(), http.StatusInternalServerError)
					}
				}
			case ReplaceValue:
				if len(relevantLabels) > 1 {
					log.Printf("Multiple relevantLabels found for replace action, taking first label: '%s', all labels: %v", relevantLabels[0], relevantLabels)
				}
				log.Printf("replacing value for %s", valkeyKey)
				if result := valkeyClient.Do(ctx, valkeyClient.B().Set().Key(valkeyKey).Value(alert.Labels[relevantLabels[0]]).Build()); result.Error() != nil {
					log.Printf("valkey error: %v", result.Error())
					http.Error(w, "valkey error: "+result.Error().Error(), http.StatusInternalServerError)
				}
			default:
				log.Printf("Unknown variable update operation type '%s' found on payload, skipping action for alert: %v", variableUpdate.Operation, alert)
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"success": "variable(s) updated"}`)
	}
}
