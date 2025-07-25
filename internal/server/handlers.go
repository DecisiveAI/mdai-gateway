package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/decisiveai/mdai-data-core/audit"
	datacore "github.com/decisiveai/mdai-data-core/variables"
	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/decisiveai/mdai-gateway/internal/adapter"
	"github.com/decisiveai/mdai-gateway/internal/httputil"
	"github.com/decisiveai/mdai-gateway/internal/manualvariables"
	"github.com/decisiveai/mdai-gateway/internal/nats"
	"github.com/decisiveai/mdai-gateway/internal/stringutil"
	"github.com/decisiveai/mdai-gateway/internal/valkey"
	"github.com/prometheus/alertmanager/template"
	"go.uber.org/zap"
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

func handleSetDeleteVariables(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
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

		event, err := nats.NewMdaiEvent(hubName, varName, string(varType), string(command), payload)
		if err != nil {
			http.Error(w, "Invalid request payload", http.StatusBadRequest)
			return
		}

		deps.Logger.Info("Publishing MdaiEvent",
			zap.String("id", event.Id),
			zap.String("name", event.Name),
			zap.String("source", event.Source))

		if _, err := nats.PublishEvents(ctx, deps.Logger, deps.EventPublisher, []eventing.MdaiEvent{*event}, deps.AuditAdapter); err != nil {
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

func handleEventsGet(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, _ *http.Request) {
		eventsMap, err := deps.AuditAdapter.HandleEventsGet(ctx)
		if err != nil {
			deps.Logger.Error("failed to get events", zap.Error(err))
			http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
			return
		}

		httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, eventsMap)
	}
}

func handleEventsPost(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		defer r.Body.Close() //nolint:errcheck

		body, err := io.ReadAll(r.Body)
		if err != nil {
			deps.Logger.Error("Failed to read request body", zap.Error(err))
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		deps.Logger.Debug("Received POST request body", zap.ByteString("body", body))

		var raw map[string]json.RawMessage
		if err := json.Unmarshal(body, &raw); err != nil {
			deps.Logger.Error("Invalid JSON in request body", zap.Error(err))
			http.Error(w, "Invalid JSON in request body", http.StatusBadRequest)
			return
		}

		switch {
		case raw["receiver"] != nil && raw["alerts"] != nil:
			handlePrometheusAlert(ctx, deps.Logger, w, body, deps.EventPublisher, deps.AuditAdapter)
		case raw["name"] != nil && raw["payload"] != nil:
			handleMdaiEvent(ctx, deps.Logger, w, body, deps.EventPublisher, deps.AuditAdapter)
		default:
			deps.Logger.Error("Unrecognized payload format", zap.ByteString("body", body))
			http.Error(w, "Invalid request body format", http.StatusBadRequest)
		}
	}
}

// Handle Prometheus Alertmanager alerts.
func handlePrometheusAlert(ctx context.Context, logger *zap.Logger, w http.ResponseWriter, body []byte, publisher eventing.Publisher, auditAdapter *audit.AuditAdapter) {
	var alertData template.Data
	if err := json.Unmarshal(body, &alertData); err != nil {
		logger.Error("Failed to unmarshal Prometheus alert", zap.Error(err))
		http.Error(w, "Invalid Prometheus alert format", http.StatusBadRequest)
		return
	}

	logger.Debug("Processing Prometheus alert",
		zap.String("receiver", alertData.Receiver),
		zap.String("status", alertData.Status),
		zap.Int("alertCount", len(alertData.Alerts)))

	wrappedAlertData := adapter.NewPromAlertWrapper(alertData)
	events, err := wrappedAlertData.ToMdaiEvents()
	if err != nil {
		logger.Error("Failed to adapt Prometheus Alert to MDAI Events", zap.Error(err))
		http.Error(w, "Failed to adapt Prometheus Alert to MDAI Events", http.StatusInternalServerError)
		return
	}

	successCount, err := nats.PublishEvents(ctx, logger, publisher, events, auditAdapter)
	switch {
	case err != nil:
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, "Published %d/%d events; some failed", successCount, len(events))
		return
	case successCount == 0:
		http.Error(w, "Failed to publish any events", http.StatusInternalServerError)
		return
	default:
		response := httputil.PrometheusAlertResponse{
			Message:    "Processed Prometheus alerts",
			Total:      len(events),
			Successful: successCount,
		}

		httputil.WriteJSONResponse(w, logger, http.StatusCreated, response)
	}
}

// Handle direct MdaiEvent submissions.
func handleMdaiEvent(ctx context.Context, logger *zap.Logger, w http.ResponseWriter, body []byte, publisher eventing.Publisher, auditAdapter *audit.AuditAdapter) {
	var event eventing.MdaiEvent
	if err := json.Unmarshal(body, &event); err != nil {
		logger.Error("Failed to unmarshal MdaiEvent", zap.Error(err))
		http.Error(w, "Invalid MdaiEvent format: "+err.Error(), http.StatusBadRequest)
		return
	}

	event.ApplyDefaults()
	if err := event.Validate(); err != nil {
		logger.Error("Failed to validate MdaiEvent", zap.Error(err))
		http.Error(w, stringutil.UpperFirst(err.Error()), http.StatusBadRequest)
		return
	}

	logger.Debug("Processing MdaiEvent",
		zap.String("id", event.Id),
		zap.String("name", event.Name),
		zap.String("source", event.Source))

	if _, err := nats.PublishEvents(ctx, logger, publisher, []eventing.MdaiEvent{event}, auditAdapter); err != nil {
		logger.Error("Failed to publish MdaiEvent", zap.Error(err))
		http.Error(w, fmt.Sprintf("Failed to publish event: %v", err), http.StatusInternalServerError)
		return
	}

	httputil.WriteJSONResponse(w, logger, http.StatusCreated, event)
}
