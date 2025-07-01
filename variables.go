package main

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	dcoreKube "github.com/decisiveai/mdai-data-core/kube"
	datacore "github.com/decisiveai/mdai-data-core/variables"
	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/decisiveai/mdai-gateway/types"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"k8s.io/api/core/v1"
)

type queryMeta struct {
	hubName      string
	variableRef  string
	variableType string
	command      string
}

// getConfiguredManualVariables  returns a map of Hub names to their corresponding ManualVariables:Types
func getAllHubsToDataMap(cmController *dcoreKube.ConfigMapController) (map[string]any, error) {
	cmController.Lock.RLock()
	defer cmController.Lock.RUnlock()

	hubMap := make(map[string]any)

	indexer := cmController.CmInformer.Informer().GetIndexer()
	hubNames := indexer.ListIndexFuncValues(dcoreKube.ByHub)
	for _, hubName := range hubNames {
		objs, err := indexer.ByIndex(dcoreKube.ByHub, hubName)
		if err != nil {
			continue
		}
		for _, obj := range objs {
			cm := obj.(*v1.ConfigMap)
			hubMap[hubName] = cm.Data
		}
	}
	return hubMap, nil
}

func HandleListVariables(ctx context.Context, cmController *dcoreKube.ConfigMapController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				logger.Error("failed to close request body: %v", zap.Error(err))
			}
		}()

		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

		var response any
		var httpStatus = http.StatusOK

		hubsVariables, err := getAllHubsToDataMap(cmController)
		if err != nil {
			WriteJSONResponse(w, http.StatusInternalServerError, response)
			return
		} else if len(hubsVariables) == 0 {
			WriteJSONResponse(w, http.StatusNotFound, response)
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

func HandleGetVariables(ctx context.Context, valkeyClient valkey.Client, cmController *dcoreKube.ConfigMapController) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				logger.Error("failed to close request body: %v", zap.Error(err))
			}
		}()

		if r.Method != http.MethodGet {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

		var response any

		qryMeta, httpStatus, err := parseHeaders(r, cmController, http.MethodGet)
		if err != nil {
			WriteJSONResponse(w, httpStatus, err.Error())
			return
		}

		valkeyAdapter := datacore.NewValkeyAdapter(valkeyClient, logger)
		var valkeyValue any

		switch qryMeta.variableType {
		case "set":
			{
				valkeyValue, err = valkeyAdapter.GetSetAsStringSlice(ctx, qryMeta.variableRef, qryMeta.hubName)
			}
		case "map":
			{
				valkeyValue, err = valkeyAdapter.GetMap(ctx, qryMeta.variableRef, qryMeta.hubName)
			}
		case "string", "int", "boolean":
			{
				valkeyValue, _, err = valkeyAdapter.GetString(ctx, qryMeta.variableRef, qryMeta.hubName)
			}
		default:
			{
				err := fmt.Errorf("unsupported variable type %v", qryMeta.variableType)
				logger.Error("event payload parsing error: %s", zap.Error(err))
			}
		}
		if err != nil {
			WriteJSONResponse(w, http.StatusInternalServerError, err.Error())
			return
		}
		response = map[string]any{
			qryMeta.variableRef: valkeyValue,
		}
		WriteJSONResponse(w, http.StatusOK, response)
	}
}

func HandleSetDeleteVariables(ctx context.Context, cmController *dcoreKube.ConfigMapController, hub eventing.EventHubInterface) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if err := r.Body.Close(); err != nil {
				logger.Error("failed to close request body: %v", zap.Error(err))
			}
		}()

		if r.Method != http.MethodPost && r.Method != http.MethodDelete {
			http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		}

		qryMeta, httpStatus, err := parseHeaders(r, cmController, r.Method)
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
		switch qryMeta.variableType {
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
				switch qryMeta.command {
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

		eventPayload := newEventPayload(qryMeta.hubName, qryMeta.variableRef, qryMeta.variableType, qryMeta.command, payload)

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
	payloadObj := eventing.ManualVariablesActionPayload{
		VariableRef: varName,
		DataType:    varType,
		Operation:   action,
		Data:        payload,
	}
	// TODO: process error
	payloadBytes, _ := json.Marshal(payloadObj)
	return eventing.MdaiEvent{
		Id:        types.CreateEventUuid(),
		Name:      strings.Join([]string{"manual_variable", action, hubName, varName}, "__"),
		HubName:   hubName,
		Timestamp: time.Now(),
		Source:    eventing.ManualVariablesEventSource,
		Payload:   string(payloadBytes),
	}

}

func WriteJSONResponse(w http.ResponseWriter, status int, v any) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(v); err != nil {
		logger.Error("failed to write response body: %v", zap.Error(err))
	}
}

func contentTypeOk(r *http.Request) bool {
	return r.Header.Get("Content-Type") == "application/json"
}

func parseHeaders(r *http.Request, cmController *dcoreKube.ConfigMapController, queryType string) (queryMeta, int, error) {
	var result = queryMeta{}

	if queryType == http.MethodPost || queryType == http.MethodDelete {
		if !contentTypeOk(r) {
			return result, http.StatusBadRequest, fmt.Errorf("wrong Content-Type header")
		}
	}

	hubsVariables, err := getAllHubsToDataMap(cmController)
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

	var command string
	switch queryType {
	case http.MethodPost:
		command = "add"
	case http.MethodDelete:
		command = "remove"
	case http.MethodGet:
	default:
		{
			return result, http.StatusBadRequest, fmt.Errorf("unsupported HTTP method")
		}
	}

	return queryMeta{
		hubName:      hubName,
		variableRef:  varName,
		variableType: varType,
		command:      command,
	}, http.StatusOK, nil

}
