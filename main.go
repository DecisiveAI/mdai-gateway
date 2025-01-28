package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/decisiveai/event-handler-webservice/types"
	"github.com/valkey-io/valkey-go"
	"gopkg.in/yaml.v3"
	"io"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
	"log"
	"net/http"
	"os"
)

const (
	namespace        = "mdai"
	configmapName    = "mdai-event-handler-config"
	valkeySecretName = "valkey-secret"

	AddElement    = "mdai/add_element"
	RemoveElement = "mdai/remove_element"
	ReplaceValue  = "mdai/replace_value"
)

var (
	errFailedToGetConfigMap    = errors.New("failed to get ConfigMap")
	errFailedToUnmarshalConfig = errors.New("failed to unmarshal config")
	errNoConfigFound           = errors.New("no config found")
)

func main() {
	ctx := context.Background()
	config, err := clientcmd.NewNonInteractiveDeferredLoadingClientConfig(
		clientcmd.NewDefaultClientConfigLoadingRules(),
		&clientcmd.ConfigOverrides{},
	).ClientConfig()
	if err != nil {
		log.Fatalf("failed to get kubernetes config: %v", err)
	}

	clientset, err := kubernetes.NewForConfig(config)
	if err != nil {
		log.Fatalf("failed to get kubernetes clientset: %v", err)
	}

	valkeyPassword := getEnvVariableWithDefault("VALKEY_PASSWORD", "")
	if valkeyPassword == "" {
		secrets, err := clientset.CoreV1().Secrets(namespace).Get(ctx, valkeySecretName, metav1.GetOptions{})
		if err != nil {
			log.Fatalf("failed to get secrets: %v", err)
		}
		valkeyPassword = string(secrets.Data["VALKEY_PASSWORD"])
	}

	valkeyClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"mdai-valkey-primary.mdai.svc.cluster.local:6379"}, // []string{string(secrets.Data["VALKEY_ENDPOINT"])},
		Password:    valkeyPassword,
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
			actionContextJSON := alert.Annotations["action_context"]
			if actionContextJSON == "" {
				log.Printf("Skipping alert because no action_context found in alert annotations, payload: %v", alert)
				continue
			}
			relevantLabelsJSON := alert.Annotations["relevant_labels"]
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
			case "firing":
				variableUpdate = actionContext.Firing.VariableUpdate
			case "resolved":
				variableUpdate = actionContext.Resolved.VariableUpdate
			}

			switch variableUpdate.Operation {
			case AddElement:
				for _, element := range relevantLabels {
					log.Printf("adding element for %s", variableUpdate.VariableRef)
					if result := valkeyClient.Do(ctx, valkeyClient.B().Sadd().Key(variableUpdate.VariableRef).Member(alert.Labels[element]).Build()); result.Error() != nil {
						log.Printf("valkey error: %v", result.Error())
						http.Error(w, "valkey error: "+result.Error().Error(), http.StatusInternalServerError)
					}
				}
			case RemoveElement:
				for _, element := range relevantLabels {
					log.Printf("removing element for %s", variableUpdate.VariableRef)
					if result := valkeyClient.Do(ctx, valkeyClient.B().Srem().Key(variableUpdate.VariableRef).Member(alert.Labels[element]).Build()); result.Error() != nil {
						log.Printf("valkey error: %v", result.Error())
						http.Error(w, "valkey error: "+result.Error().Error(), http.StatusInternalServerError)
					}
				}
			case ReplaceValue:
				if len(relevantLabels) > 1 {
					log.Printf("Multiple relevantLabels found for replace action, taking first label: '%s', all labels: %v", relevantLabels[0], relevantLabels)
				}
				log.Printf("replacing value for %s", variableUpdate.VariableRef)
				if result := valkeyClient.Do(ctx, valkeyClient.B().Set().Key(variableUpdate.VariableRef).Value(alert.Labels[relevantLabels[0]]).Build()); result.Error() != nil {
					log.Printf("valkey error: %v", result.Error())
					http.Error(w, "valkey error: "+result.Error().Error(), http.StatusInternalServerError)
				}
			}
		}
		w.WriteHeader(http.StatusOK)
		fmt.Fprintf(w, `{"success": "variable(s) updated"}`)
	}
}

func getConfig(ctx context.Context, clientset kubernetes.Interface) (*types.Config, error) {
	configMap, err := clientset.CoreV1().ConfigMaps(namespace).Get(ctx, configmapName, metav1.GetOptions{})
	if err != nil {
		log.Printf("failed to get configmap: %v", err)
		return nil, errFailedToGetConfigMap
	}

	configYAML, configYAMLExists := configMap.Data["config.yaml"]
	if !configYAMLExists {
		log.Printf("no config found")
		return nil, errNoConfigFound
	}

	var config types.Config
	if err := yaml.Unmarshal([]byte(configYAML), &config); err != nil {
		log.Printf("failed to unmarshal config: %v", err)
		return nil, errFailedToUnmarshalConfig
	}
	return &config, nil
}
