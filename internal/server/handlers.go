package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/decisiveai/mdai-data-core/eventing"
	"github.com/decisiveai/mdai-data-core/eventing/config"
	"github.com/decisiveai/mdai-data-core/eventing/publisher"
	datacore "github.com/decisiveai/mdai-data-core/variables"
	"github.com/decisiveai/mdai-gateway/internal/adapter"
	"github.com/decisiveai/mdai-gateway/internal/httputil"
	"github.com/decisiveai/mdai-gateway/internal/manualvariables"
	"github.com/decisiveai/mdai-gateway/internal/nats"
	"github.com/decisiveai/mdai-gateway/internal/stringutil"
	"github.com/decisiveai/mdai-gateway/internal/valkey"
	"github.com/prometheus/alertmanager/notify/webhook"
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

func handleAuditEventsGet(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
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

func handlePromAlertsPost(deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		const maxBody = 10 << 20 // 10 MiB, TODO make this configurable
		r.Body = http.MaxBytesReader(w, r.Body, maxBody)
		defer r.Body.Close() //nolint:errcheck

		var msg webhook.Message
		dec := json.NewDecoder(r.Body)
		dec.DisallowUnknownFields()

		if err := dec.Decode(&msg); err != nil {
			var mbe *http.MaxBytesError
			if errors.As(err, &mbe) {
				http.Error(w, "request body too large (max 10MiB)", http.StatusRequestEntityTooLarge)
				return
			}
			deps.Logger.Error("Failed to decode Alertmanager JSON", zap.Error(err))
			http.Error(w, "invalid Alertmanager payload", http.StatusBadRequest)
			return
		}
		// Ensure single JSON value (no trailing junk)
		if err := dec.Decode(&struct{}{}); err != io.EOF {
			http.Error(w, "request must contain a single JSON object", http.StatusBadRequest)
			return
		}

		deps.Logger.Debug("Received /alerts/alertmanager POST", zap.Any("msg", msg))

		handlePrometheusAlerts(r.Context(), deps.Logger, w, *msg.Data, deps.EventPublisher, deps.AuditAdapter, deps.Deduper)
	}
}

// Handle Prometheus Alertmanager alerts.
func handlePrometheusAlerts(ctx context.Context, logger *zap.Logger, w http.ResponseWriter, alertData template.Data, p publisher.Publisher, auditAdapter *audit.AuditAdapter, deduper *adapter.Deduper) {
	logger.Debug("Processing Prometheus alert",
		zap.String("receiver", alertData.Receiver),
		zap.String("status", alertData.Status),
		zap.Int("alertCount", len(alertData.Alerts)))

	wrappedAlertData := adapter.NewPromAlertWrapper(alertData, logger, deduper)
	eventPerSubjects, skipped, err := wrappedAlertData.ToMdaiEvents()
	if err != nil {
		logger.Error("Failed to adapt Prometheus Alert to MDAI Events", zap.Error(err))
		http.Error(w, "Failed to adapt Prometheus Alert to MDAI Events", http.StatusInternalServerError)
		return
	}

	successCount, err := nats.PublishEvents(ctx, logger, p, eventPerSubjects, auditAdapter)
	switch {
	case err != nil:
		w.WriteHeader(http.StatusAccepted)
		_, _ = fmt.Fprintf(w, "Published %d/%d eventPerSubjects; some failed", successCount, len(eventPerSubjects))
		return
	default:
		response := httputil.PrometheusAlertResponse{
			Message:    "Processed Prometheus alerts",
			Total:      len(alertData.Alerts),
			Successful: successCount,
			Skipped:    skipped,
		}

		httputil.WriteJSONResponse(w, logger, http.StatusCreated, response)
	}
}

// subjectFromAlert creates a subject from a mdai event and variable key. Prefix has to be added later at eventing package.
func subjectFromVarsEvent(event eventing.MdaiEvent, varkey string) eventing.MdaiEventSubject {
	return eventing.MdaiEventSubject{
		Type: eventing.VarEventType,
		Path: config.SafeToken(event.HubName) + "." + config.SafeToken(varkey),
	}
}
