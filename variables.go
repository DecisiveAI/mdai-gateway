package main

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"os"

	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"
	"k8s.io/client-go/tools/clientcmd"
)

const manualEnvConfigMapNamePostfix = "-manual-variables"

// getConfiguredManualVariables  reurns a map of Hub names to their corresponding ManualVariables:Types
func getConfiguredManualVariables(ctx context.Context) (map[string]any, error) {
	//config, err := rest.InClusterConfig()
	kubeconfig, err := os.UserHomeDir()
	if err != nil {
		logger.Error("Failed to load config", zap.Error(err))
		return nil, err
	}

	config, err := clientcmd.BuildConfigFromFlags("", kubeconfig+"/.kube/config")
	if err != nil {
		logger.Error("Failed to build k8s config", zap.Error(err))
		return nil, err
	}
	client, err := dynamic.NewForConfig(config)
	if err != nil {
		logger.Error("Failed to create k8s dynamic client", zap.Error(err))
		return nil, err
	}
	gvrCR := schema.GroupVersionResource{
		Group:    "hub.mydecisive.ai",
		Version:  "v1",
		Resource: "mdaihubs",
	}
	hubs, err := client.Resource(gvrCR).List(ctx, v1.ListOptions{})
	if err != nil {
		panic(err)
	}
	gvrConfigMap := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "configmaps",
	}

	configMaps, err := client.Resource(gvrConfigMap).List(ctx, v1.ListOptions{})
	if err != nil {
		logger.Error("Failed to list ConfigMap", zap.Error(err))
		return nil, err
	}

	hubMap := make(map[string]any)
	for _, hub := range hubs.Items {
		for _, configMap := range configMaps.Items {
			if configMap.GetName() == hub.GetName()+manualEnvConfigMapNamePostfix {
				data, found, err := unstructured.NestedStringMap(configMap.Object, "data")
				if err != nil || !found {
					logger.Info("data field missing or invalid", zap.Any("hub", hub.GetName()), zap.Error(err))
				}
				hubMap[hub.GetName()] = data
			}
		}
	}
	if len(hubMap) == 0 {
		return hubMap, errors.New("no manual variables found")
	}
	return hubMap, nil
}

func HandleListVariables(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var response any
		var httpStatus = http.StatusOK

		hubsVariables, err := getConfiguredManualVariables(ctx)
		if err != nil && hubsVariables == nil {
			httpStatus = http.StatusInternalServerError
		}

		hub := r.PathValue("hub")
		if hub == "" {
			response = hubsVariables
		} else {
			if hubVariables := hubsVariables[hub]; hubVariables != nil {
				response = hubVariables
			}
		}
		WriteJSONResponse(w, httpStatus, response)

	}

}

func WriteJSONResponse(w http.ResponseWriter, status int, v any) error {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)

	return json.NewEncoder(w).Encode(v)
}
