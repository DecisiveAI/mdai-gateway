package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"maps"
	"net/http"
	"slices"

	"github.com/decisiveai/event-handler-webservice/types"
	"github.com/valkey-io/valkey-go"
	"gopkg.in/yaml.v3"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/tools/clientcmd"
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

	secrets, err := clientset.CoreV1().Secrets(namespace).Get(ctx, valkeySecretName, metav1.GetOptions{})
	if err != nil {
		log.Fatalf("failed to get secrets: %v", err)
	}

	valkeyClient, err := valkey.NewClient(valkey.ClientOption{
		InitAddress: []string{"mdai-valkey-primary.mdai.svc.cluster.local:6379"}, // []string{string(secrets.Data["VALKEY_ENDPOINT"])},
		Password:    string(secrets.Data["VALKEY_PASSWORD"]),
	})
	if err != nil {
		log.Fatalf("failed to get valkey client: %v", err)
	}
	http.HandleFunc("/alerts", handleAlertsPost(ctx, clientset, valkeyClient))

	log.Println("Starting server on :8081")
	log.Fatal(http.ListenAndServe(":8081", nil))
}

func handleAlertsPost(ctx context.Context, clientset kubernetes.Interface, valkeyClient valkey.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
			return
		}

		config, err := getConfig(ctx, clientset)
		if err != nil {
			http.Error(w, err.Error(), http.StatusInternalServerError)
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			log.Printf("failed to read body: %v", err)
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		var payload types.AlertManagerPayload
		if err := json.Unmarshal(body, &payload); err != nil {
			log.Printf("invalid json: %v", body)
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}
		evaluationsMap := make(map[string]types.Evaluation, len(config.Evaluations))
		for _, evaluation := range config.Evaluations {
			evaluationsMap[string(evaluation.Name)] = evaluation
		}
		evaluations := slices.Collect(maps.Keys(evaluationsMap))

		variablesMap := make(map[string]types.Variable, len(config.Variables))
		for _, variable := range config.Variables {
			variablesMap[variable.StorageKey] = variable
		}

		for _, alert := range payload.Alerts {
			if !slices.Contains(evaluations, alert.Labels["alertname"]) {
				log.Printf("alert %s not found in evaluations", alert.Labels["alertname"])
				continue
			}
			alertName := alert.Labels["alertname"]
			var variableUpdate *types.VariableUpdate
			switch alert.Status {
			case "firing":
				variableUpdate = evaluationsMap[alertName].Status.Firing.VariableUpdate
			case "resolved":
				variableUpdate = evaluationsMap[alertName].Status.Resolved.VariableUpdate
			}

			var result valkey.ValkeyResult
			switch variableUpdate.Operation {
			case AddElement:
				result = valkeyClient.Do(ctx, valkeyClient.B().Sadd().Key(variableUpdate.VariableRef).Member(alert.Annotations["service_name"]).Build())
				log.Printf("adding element for %s", variableUpdate.VariableRef)
			case RemoveElement:
				result = valkeyClient.Do(ctx, valkeyClient.B().Srem().Key(variableUpdate.VariableRef).Member(alert.Annotations["service_name"]).Build())
				log.Printf("removing element for %s", variableUpdate.VariableRef)
			case ReplaceValue:
				log.Printf("replacing value for %s", variableUpdate.VariableRef)
				result = valkeyClient.Do(ctx, valkeyClient.B().Set().Key(variableUpdate.VariableRef).Value(alert.Annotations["service_name"]).Build())
			}
			if result.Error() != nil {
				log.Printf("valkey error: %v", result.Error())
				http.Error(w, "valkey error: "+result.Error().Error(), http.StatusInternalServerError)
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
