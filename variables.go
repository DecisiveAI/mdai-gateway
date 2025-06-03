package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
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

const (
	manualEnvConfigMapNamePostfix = "-manual-variables"
	staticVariablesEventSource    = "static_variable_api"
)

type QueryMeta struct {
	HubName      string `json:"hubName"`
	VariableName string `json:"variableName"`
	VariableType string `json:"variableTame"`
	Command      string `json:"command"`
}

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
	return hubMap, nil
}

func HandleListVariables(ctx context.Context, k8sClient dynamic.Interface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var response any
		var httpStatus = http.StatusOK

		hubsVariables, err := getConfiguredManualVariables(ctx, k8sClient)
		if err != nil {
			httpStatus = http.StatusInternalServerError
			WriteJSONResponse(w, httpStatus, response)
			return
		} else if len(hubsVariables) == 0 {
			httpStatus = http.StatusNotFound
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
		defer r.Body.Close()

		var response any

		queryMeta, httpStatus, err := parseHeaders(ctx, w, r, k8sClient, "GET")
		if err != nil {
			WriteJSONResponse(w, httpStatus, err.Error())
			return
		}

		valkeyAdapter := datacore.NewValkeyAdapter(valkeyClient, zapr.NewLogger(logger))
		var valkeyValue any

		switch queryMeta.VariableType {
		case "set":
			{
				valkeyValue, err = valkeyAdapter.GetSetAsStringSlice(ctx, queryMeta.VariableName, queryMeta.HubName)
			}
		case "map":
			{
				valkeyValue, err = valkeyAdapter.GetMap(ctx, queryMeta.VariableName, queryMeta.HubName)
			}
		case "string", "int", "boolean":
			{
				valkeyValue, _, err = valkeyAdapter.GetString(ctx, queryMeta.VariableName, queryMeta.HubName)
			}
		}
		if err != nil {
			WriteJSONResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		response = map[string]any{
			queryMeta.VariableName: valkeyValue,
		}
		WriteJSONResponse(w, http.StatusOK, response)

	}
}

func HandleSetVariables(ctx context.Context, k8sClient dynamic.Interface, hub *eventing.EventHub) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close()

		var httpStatus = http.StatusOK

		queryMeta, httpStatus, err := parseHeaders(ctx, w, r, k8sClient, "POST")
		if err != nil {
			WriteJSONResponse(w, httpStatus, err.Error())
			return
		}

		var raw map[string]json.RawMessage
		if err := json.NewDecoder(r.Body).Decode(&raw); err != nil && raw["data"] == nil {
			http.Error(w, "Invalid request payload. Expect {\"data\": any}", http.StatusBadRequest)
			return
		}
		var data = raw["data"]

		var payload any
		var payloadTmp any
		if err := json.Unmarshal(data, &payloadTmp); err != nil {
			http.Error(w, "Invalid request payload. A slice of strings expected", http.StatusBadRequest)
			return
		}
		switch queryMeta.VariableType {
		case "set":
			{
				switch payloadTmp.(type) {
				case []interface{}:
					var payloadList []string
					if err := json.Unmarshal(data, &payloadList); err != nil {
						http.Error(w, "Invalid request payload. List expected", http.StatusBadRequest)
						return
					}
					payload = payloadList
				default:
					http.Error(w, "Invalid request payload. List expected", http.StatusBadRequest)
					return
				}
			}
		case "map":
			{
				switch queryMeta.Command {
				case "add":
					{
						switch payloadTmp.(type) {
						case map[string]interface{}:
							var payloadMap map[string]string
							if err := json.Unmarshal(data, &payloadMap); err != nil {
								http.Error(w, "Invalid request payload. Map expected", http.StatusBadRequest)
								return
							}
							payload = payloadMap
						default:
							http.Error(w, "Invalid request payload. Map expected", http.StatusBadRequest)
							return
						}
					}
				case "remove":
					{
						switch payloadTmp.(type) {
						case []interface{}:
							var payloadList []string
							if err := json.Unmarshal(data, &payloadList); err != nil {
								http.Error(w, "Invalid request payload. List expected", http.StatusBadRequest)
								return
							}
							payload = payloadList
						default:
							http.Error(w, "Invalid request payload. List expected", http.StatusBadRequest)
							return
						}
					}
				default:
					{
						http.Error(w, "Invalid command", http.StatusBadRequest)
						return
					}
				}
			}
		case "string":
			{
				switch payloadTmp.(type) {
				case string:
					var payloadString string
					if err := json.Unmarshal(data, &payloadString); err != nil {
						http.Error(w, "Invalid request payload. String expected", http.StatusBadRequest)
						return
					}
					payload = payloadString
				default:
					http.Error(w, "Invalid request payload. String expected", http.StatusBadRequest)
					return
				}
			}
		case "int":
			{
				switch payloadTmp.(type) {
				case float64: // that's how all int numbers types appear
					var payloadInt int
					if err := json.Unmarshal(data, &payloadInt); err != nil {
						http.Error(w, "Invalid request payload. Int expected", http.StatusBadRequest)
						return
					}
					payload = strconv.Itoa(payloadInt)
				default:
					http.Error(w, "Invalid request payload. Int expected", http.StatusBadRequest)
					return
				}
			}
		case "boolean":
			{
				switch payloadTmp.(type) {
				case bool:
					var payloadBool bool
					if err := json.Unmarshal(data, &payloadBool); err != nil {
						http.Error(w, "Invalid request payload. Bool expected", http.StatusBadRequest)
						return
					}
					payload = strconv.FormatBool(payloadBool)
				default:
					http.Error(w, "Invalid request payload. Bool expected", http.StatusBadRequest)
					return
				}
			}
		}

		eventPayload := newEventPayload(queryMeta.HubName, queryMeta.VariableName, queryMeta.VariableType, queryMeta.Command, payload)

		logger.Info("POST:", zap.Any("payload", payload))
		WriteJSONResponse(w, http.StatusOK, eventPayload)

		logger.Info("Processing MdaiEvent",
			zap.String("id", eventPayload.Id),
			zap.String("name", eventPayload.Name),
			zap.String("source", eventPayload.Source))

		if err := hub.PublishMessage(eventPayload); err != nil {
			logger.Error("Failed to publish MdaiEvent", zap.Error(err))
			http.Error(w, fmt.Sprintf("Failed to publish event: %v", err), http.StatusInternalServerError)
			return
		}

	}
}

func newEventPayload(hubName string, varName string, varType string, action string, payload any) eventing.MdaiEvent {
	payloadObj := eventing.StaticVariablesActionPayload{
		Hub:       hubName,
		Variable:  varName,
		Type:      varType,
		Operation: action,
		Data:      payload,
	}
	// TODO: process error
	payloadBytes, _ := json.Marshal(payloadObj)
	return eventing.MdaiEvent{
		Id:        types.CreateEventUuid(),
		Name:      strings.Join([]string{"static_variable", action, hubName, varName}, "__"),
		HubName:   hubName,
		Timestamp: time.Now(),
		Source:    staticVariablesEventSource,
		Payload:   string(payloadBytes),
	}

}

func WriteJSONResponse(w http.ResponseWriter, status int, v any) error {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)

	return json.NewEncoder(w).Encode(v)
}

func contentTypeOk(r *http.Request) bool {
	return r.Header.Get("Content-Type") == "application/json"
}

func parseHeaders(ctx context.Context, w http.ResponseWriter, r *http.Request, k8sClient dynamic.Interface, queryType string) (QueryMeta, int, error) {
	var result = QueryMeta{}

	if queryType == "POST" {
		if !contentTypeOk(r) {
			return result, http.StatusBadRequest, fmt.Errorf("wrong Content-Type header")
		}
	}

	hubsVariables, err := getConfiguredManualVariables(ctx, k8sClient)
	if err != nil {
		return result, http.StatusInternalServerError, err
	} else if len(hubsVariables) == 0 {
		return result, http.StatusNotFound, fmt.Errorf("no hubs with manual variables found")
	}

	hubName := r.PathValue("hubName")
	varName := r.PathValue("varName")
	if hubName == "" || varName == "" {
		return result, http.StatusBadRequest, fmt.Errorf("missing hub or variable name")
	}

	hubFound := hubsVariables[hubName]
	if hubFound == nil {
		return result, http.StatusNotFound, fmt.Errorf("hub not found")
	}

	varType, ok := hubFound.(map[string]string)[varName]
	if !ok {
		return result, http.StatusNotFound, fmt.Errorf("variable not found")
	}

	command := r.PathValue("command")
	if queryType == "POST" {
		if command != "add" && command != "remove" {
			return result, http.StatusBadRequest, fmt.Errorf("unsupported command")
		}
	}

	return QueryMeta{
		HubName:      hubName,
		VariableName: varName,
		VariableType: varType,
		Command:      command,
	}, http.StatusOK, nil

}
