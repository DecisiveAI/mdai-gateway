package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/mydecisive/mdai-data-core/audit"
	"github.com/mydecisive/mdai-data-core/eventing"
	"github.com/mydecisive/mdai-data-core/eventing/config"
	"github.com/mydecisive/mdai-data-core/eventing/publisher"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	"github.com/mydecisive/mdai-gateway/internal/httputil"
	"github.com/mydecisive/mdai-gateway/internal/nats"
	"github.com/prometheus/alertmanager/notify/webhook"
	"github.com/prometheus/alertmanager/template"
	"go.uber.org/zap"
)

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

	commitDedupe := func(eps adapter.EventPerSubject) {
		deduper.UpdateIfNewer(eps.Event.SourceID, eps.Event.Timestamp)
	}

	successCount, err := nats.PublishEvents(ctx, logger, p, eventPerSubjects, auditAdapter, commitDedupe)
	switch {
	case err != nil:
		logger.Error("Failed to publish some alert events", zap.Error(err),
			zap.Int("successful", successCount), zap.Int("total", len(eventPerSubjects)))
		http.Error(w, fmt.Sprintf("published %d/%d events; delivery failed, expecting retry", successCount, len(eventPerSubjects)), http.StatusInternalServerError)
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
