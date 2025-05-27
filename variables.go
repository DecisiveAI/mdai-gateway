package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strings"
	"time"

	"github.com/go-logr/zapr"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/apis/meta/v1/unstructured"
	"k8s.io/apimachinery/pkg/runtime/schema"
	"k8s.io/client-go/dynamic"

	"github.com/decisiveai/event-handler-webservice/types"
	"github.com/decisiveai/event-hub-poc/eventing"
	datacore "github.com/decisiveai/mdai-data-core/variables"
)

const manualEnvConfigMapNamePostfix = "-manual-variables"

// getConfiguredManualVariables  returns a map of Hub names to their corresponding ManualVariables:Types
func getConfiguredManualVariables(ctx context.Context, k8sClient dynamic.Interface) (map[string]any, error) {
	// TODO: make ConfiMap fetcher async
	gvrCR := schema.GroupVersionResource{
		Group:    "hub.mydecisive.ai",
		Version:  "v1",
		Resource: "mdaihubs",
	}
	hubs, err := k8sClient.Resource(gvrCR).List(ctx, v1.ListOptions{})
	if err != nil {
		panic(err)
	}
	gvrConfigMap := schema.GroupVersionResource{
		Group:    "",
		Version:  "v1",
		Resource: "configmaps",
	}

	configMaps, err := k8sClient.Resource(gvrConfigMap).List(ctx, v1.ListOptions{})
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

func HandleListVariables(ctx context.Context, k8sClient dynamic.Interface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var response any
		var httpStatus = http.StatusOK

		hubsVariables, err := getConfiguredManualVariables(ctx, k8sClient)
		if err != nil && hubsVariables == nil {
			httpStatus = http.StatusInternalServerError
			WriteJSONResponse(w, httpStatus, response)
			return
		}

		hubName := r.PathValue("hubName")
		if hubName == "" {
			response = hubsVariables
		} else {
			if hubVariables := hubsVariables[hubName]; hubVariables != nil {
				response = hubVariables
			} else {
				response = "Hub not found"
				httpStatus = http.StatusNotFound
			}
		}
		WriteJSONResponse(w, httpStatus, response)

	}

}

func HandleGetVariables(ctx context.Context, valkeyClient valkey.Client, k8sClient dynamic.Interface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var response any

		hubsVariables, err := getConfiguredManualVariables(ctx, k8sClient)
		if err != nil && hubsVariables == nil {
			WriteJSONResponse(w, http.StatusInternalServerError, response)
			return
		}
		hubName := r.PathValue("hubName")
		varName := r.PathValue("varName")
		// TODO: implement all variables handling (?)
		if hubName == "" || varName == "" {
			WriteJSONResponse(w, http.StatusBadRequest, "Missing hub or variable name")
			return
		}

		valkeyAdapter := datacore.NewValkeyAdapter(valkeyClient, zapr.NewLogger(logger))
		hub := hubsVariables[hubName]
		if hub == nil {
			WriteJSONResponse(w, http.StatusNotFound, "Hub not found")
			return
		}
		varType, ok := hub.(map[string]string)[varName]
		if !ok {
			WriteJSONResponse(w, http.StatusNotFound, "Variable not found")
			return
		}
		var valkeyValue any

		switch varType {
		case "set":
			{
				valkeyValue, err = valkeyAdapter.GetSetAsStringSlice(ctx, varName, hubName)
			}
		case "map":
			{
				valkeyValue, err = valkeyAdapter.GetMap(ctx, varName, hubName)
			}
		case "string", "int", "boolean":
			{
				valkeyValue, _, err = valkeyAdapter.GetString(ctx, varName, hubName)
			}
		}
		if err != nil {
			WriteJSONResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		response = map[string]any{
			varName: valkeyValue,
		}
		WriteJSONResponse(w, http.StatusOK, response)

	}
}

func HandleSetVariables(ctx context.Context, k8sClient dynamic.Interface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		var response any

		if !headersOk(r) {
			WriteJSONResponse(w, http.StatusBadRequest, "Wrong Content-Type header")
			return
		}

		hubsVariables, err := getConfiguredManualVariables(ctx, k8sClient)
		if err != nil && hubsVariables == nil {
			WriteJSONResponse(w, http.StatusInternalServerError, response)
			return
		}
		hubName := r.PathValue("hubName")
		varName := r.PathValue("varName")
		if hubName == "" || varName == "" {
			WriteJSONResponse(w, http.StatusBadRequest, "Missing hub or variable name")
			return
		}

		hub := hubsVariables[hubName]
		if hub == nil {
			WriteJSONResponse(w, http.StatusNotFound, "Hub not found")
			return
		}

		varType, ok := hub.(map[string]string)[varName]
		if !ok {
			WriteJSONResponse(w, http.StatusNotFound, "Variable not found")
			return
		}

		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "Invalid request payload. Not map[string]json.RawMessage", http.StatusBadRequest)
			return
		}

		var payload any
		var eventPayload eventing.MdaiEvent
		switch varType {
		case "set":
			{
				var payloadSet []string
				if err := json.Unmarshal(raw["value"], &payloadSet); err != nil {
					http.Error(w, "Invalid request payload. String list expected", http.StatusBadRequest)
					return
				}
				payload = payloadSet
				eventPayload = newEventPayload(hubName, varName, varType, "set", payload)
			}
		case "map":
			{
				var payloadMap map[string]string
				if err := json.Unmarshal(raw["value"], &payloadMap); err != nil {
					http.Error(w, "Invalid request payload. Map expected", http.StatusBadRequest)
					return
				}
				payload = payloadMap
				eventPayload = newEventPayload(hubName, varName, varType, "set", payload)
			}
		case "string", "int", "boolean":
			{
				var payloadString string
				if err := json.Unmarshal(raw["value"], &payloadString); err != nil {
					http.Error(w, "Invalid request payload. String expected", http.StatusBadRequest)
					return
				}
				payload = payloadString
				eventPayload = newEventPayload(hubName, varName, varType, "set", payload)
			}
		}
		logger.Info("POST:", zap.Any("payload", payload))
		WriteJSONResponse(w, http.StatusOK, eventPayload)

	}
}

func newEventPayload(hubName string, varName string, varType string, action string, payload any) eventing.MdaiEvent {
	return eventing.MdaiEvent{
		Id:        types.CreateEventUuid(),
		Name:      strings.Join([]string{"static_variable", action, hubName, varName}, "__"),
		HubName:   hubName,
		Timestamp: time.Now(),
		Source:    "static_variable_api",
		Payload: fmt.Sprintf(
			"{hub: %s, variable: %s, type: %s, operation: %s, value: %s}",
			hubName,
			varName,
			varType,
			action,
			payload),
	}

}

func WriteJSONResponse(w http.ResponseWriter, status int, v any) error {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)

	return json.NewEncoder(w).Encode(v)
}

func headersOk(r *http.Request) bool {
	return r.Header.Get("Content-Type") == "application/json"
}
