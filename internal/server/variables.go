package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/mydecisive/mdai-data-core/eventing"
	datacore "github.com/mydecisive/mdai-data-core/variables"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	"github.com/mydecisive/mdai-gateway/internal/httputil"
	"github.com/mydecisive/mdai-gateway/internal/manualvariables"
	"github.com/mydecisive/mdai-gateway/internal/nats"
	"github.com/mydecisive/mdai-gateway/internal/stringutil"
	"github.com/mydecisive/mdai-gateway/internal/valkey"
	"go.uber.org/zap"
	"net/http"
)

func handleListAllVariables(_ context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubsVariables, err := deps.ConfigMapController.GetAllHubsToDataMap()
		if err != nil {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusInternalServerError, "failed to fetch manual variables")
			return
		}
		if len(hubsVariables) == 0 {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusNotFound, "no hubs with manual variables found")
			return
		}

		httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, hubsVariables)
	}
}

func handleListHubVariables(_ context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubName := r.PathValue("hubName")
		if hubName == "" {
			http.Error(w, "hub name required", http.StatusBadRequest)
			return
		}
		hubsVariables, err := deps.ConfigMapController.GetAllHubsToDataMap()
		if err != nil {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusInternalServerError, "failed to fetch manual variables")
			return
		}
		if len(hubsVariables) == 0 {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusNotFound, "no hubs with manual variables found")
			return
		}
		if hubVariables, exists := hubsVariables[hubName]; exists {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, hubVariables)
			return
		}
		httputil.WriteJSONResponse(w, deps.Logger, http.StatusNotFound, "Hub not found")
	}
}

func handleGetVariables(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		hubName := r.PathValue("hubName")
		varName := r.PathValue("varName")
		if hubName == "" || varName == "" {
			http.Error(w, "hub and var name required", http.StatusBadRequest)
			return
		}

		hubsVariables, err := deps.ConfigMapController.GetAllHubsToDataMap()
		if err != nil {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusInternalServerError, "failed to fetch manual variables")
			return
		}
		if len(hubsVariables) == 0 {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusNotFound, "no hubs with manual variables found")
			return
		}

		varType, err := manualvariables.GetVarType(hubName, varName, hubsVariables)
		if err != nil {
			status := http.StatusInternalServerError
			var httpErr manualvariables.HTTPError
			if errors.As(err, &httpErr) {
				status = httpErr.HTTPStatus()
			}
			httputil.WriteJSONResponse(w, deps.Logger, status, err.Error())
			return
		}

		valkeyValue, err := valkey.GetValue(ctx, datacore.NewValkeyAdapter(deps.ValkeyClient, deps.Logger), varName, varType, hubName)
		if err != nil {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusInternalServerError, err.Error())
			return
		}

		response := map[string]any{varName: valkeyValue}
		httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, response)
	}
}

func handleSetDeleteVariables(ctx context.Context, deps HandlerDeps) http.HandlerFunc { //nolint:funlen
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close() //nolint:errcheck

		hubName := r.PathValue("hubName")
		varName := r.PathValue("varName")
		if hubName == "" || varName == "" {
			http.Error(w, "hub and var name required", http.StatusBadRequest)
			return
		}

		hubsVariables, err := deps.ConfigMapController.GetAllHubsToDataMap()
		if err != nil {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusInternalServerError, "failed to fetch manual variables")
			return
		}
		if len(hubsVariables) == 0 {
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusNotFound, "no hubs with manual variables found")
			return
		}

		varType, err := manualvariables.GetVarType(hubName, varName, hubsVariables)
		if err != nil {
			status := http.StatusInternalServerError
			var httpErr manualvariables.HTTPError
			if errors.As(err, &httpErr) {
				status = httpErr.HTTPStatus()
			}
			httputil.WriteJSONResponse(w, deps.Logger, status, err.Error())
			return
		}

		var raw map[string]json.RawMessage
		if err = json.NewDecoder(r.Body).Decode(&raw); err != nil {
			http.Error(w, "Invalid JSON format in request payload", http.StatusBadRequest)
			return
		}

		if raw["data"] == nil {
			http.Error(w, `Invalid request payload. expect {"data": any}`, http.StatusBadRequest)
			return
		}

		command := valkey.CommandAdd
		if r.Method == http.MethodDelete {
			command = valkey.CommandDel
		}

		parser, err := valkey.GetParser(varType, command)
		if err != nil {
			http.Error(w, "Invalid request payload: "+err.Error(), http.StatusBadRequest)
			return
		}

		payload, err := parser(raw["data"])
		if err != nil {
			http.Error(w, "Invalid request payload: "+stringutil.UpperFirst(err.Error()), http.StatusBadRequest)
			return
		}

		event, err := eventing.NewMdaiEvent(hubName, varName, string(varType), string(command), payload)
		if err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		subject := subjectFromVarsEvent(*event, varName)

		deps.Logger.Info("Publishing MdaiEvent",
			zap.String("id", event.ID),
			zap.String("name", event.Name),
			zap.String("source", event.Source),
			zap.String("subject", subject.String()),
		)

		if _, err := nats.PublishEvents(ctx, deps.Logger, deps.EventPublisher, []adapter.EventPerSubject{{Event: *event, Subject: subject}}, deps.AuditAdapter); err != nil {
			deps.Logger.Error("Failed to publish MdaiEvent", zap.Error(err))
			http.Error(w, fmt.Sprintf("Failed to publish event: %v", err), http.StatusInternalServerError)
			return
		}

		status := http.StatusOK
		if r.Method == http.MethodPost {
			status = http.StatusCreated
		}

		httputil.WriteJSONResponse(w, deps.Logger, status, event)
	}
}
