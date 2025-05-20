package main

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"sort"

	"github.com/decisiveai/mdai-data-core/audit"
	datacore "github.com/decisiveai/mdai-data-core/variables"
	mdaiv1 "github.com/decisiveai/mdai-operator/api/v1"
	"github.com/go-logr/zapr"
	"github.com/prometheus/alertmanager/template"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func handleAlertsPost(ctx context.Context, logger *zap.Logger, valkeyClient valkey.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		processMutex.Lock()
		defer processMutex.Unlock()

		if r.Method != http.MethodPost {
			http.Error(w, "Only POST method is supported", http.StatusMethodNotAllowed)
			return
		}

		body, err := io.ReadAll(r.Body)
		if err != nil {
			logger.Info("failed to read body", zap.Error(err))
			http.Error(w, "Failed to read request body", http.StatusBadRequest)
			return
		}

		logger.Info("Received payload", zap.ByteString("payload", body))

		var payload template.Data
		if err := json.Unmarshal(body, &payload); err != nil {
			logger.Info("Invalid JSON", zap.ByteString("body", body), zap.Error(err))
			http.Error(w, "Invalid JSON", http.StatusBadRequest)
			return
		}

		// processing the alerts in chronological order
		sort.Slice(payload.Alerts, func(i, j int) bool {
			return payload.Alerts[i].StartsAt.Before(payload.Alerts[j].StartsAt)
		})

		var valkeyErrors error
		auditAdapter := audit.NewAuditAdapter(zapr.NewLogger(logger), valkeyClient, valkeyAuditStreamExpiry)
		for _, alert := range payload.Alerts {
			hubName := alert.Annotations[HubName]
			if hubName == "" {
				logger.Info("Skipping alert because no hub_name found in alert annotations, payload: %v", zap.Any("alert", alert))
				continue
			}

			actionContextJSON := alert.Annotations[actionContextAnnotationsKey]
			if actionContextJSON == "" {
				logger.Info("Skipping alert, missing action_context", zap.Any("alert", alert))
				continue
			}
			relevantLabelsJSON := alert.Annotations[relevantLabelsAnnotationsKey]
			if relevantLabelsJSON == "" {
				logger.Info("Skipping alert, missing relevant_labels", zap.Any("alert", alert))
				continue
			}
			var actionContext mdaiv1.PrometheusAlertEvaluationStatus
			if err := json.Unmarshal([]byte(actionContextJSON), &actionContext); err != nil {
				logger.Info("Could not unmarshal action_context", zap.Any("alert", alert), zap.Error(err))
				continue
			}
			relevantLabels := make([]string, 0)
			if err := json.Unmarshal([]byte(relevantLabelsJSON), &relevantLabels); err != nil {
				logger.Info("Could not unmarshal relevant_labels", zap.Any("alert", alert), zap.Error(err))
				continue
			}

			logger.Info("Processing alert", zap.Any("alert", alert))

			var variableUpdate *mdaiv1.VariableUpdate
			switch alert.Status {
			case firingStatus:
				if actionContext.Firing == nil || actionContext.Firing.VariableUpdate == nil {
					logger.Error("No firing context found for alert", zap.Any("alert", alert))
					continue
				}
				variableUpdate = actionContext.Firing.VariableUpdate
			case resolvedStatus:
				if actionContext.Resolved == nil || actionContext.Resolved.VariableUpdate == nil {
					logger.Error("No resolved context found for alert", zap.Any("alert", alert))
					continue
				}
				variableUpdate = actionContext.Resolved.VariableUpdate
			default:
				logger.Error("Invalid alert status: %s, payload: %v", zap.Any("alert", alert))
				continue
			}

			dataAdapter := datacore.NewValkeyAdapter(valkeyClient, zapr.NewLogger(logger), hubName)
			mdaiHubEvent := auditAdapter.CreateHubEvent(relevantLabels, alert)
			logger.Info("AUDIT: Updated variable", zap.Inline(mdaiHubEvent))
			if err := auditAdapter.InsertAuditLogEventFromEvent(ctx, mdaiHubEvent); err != nil {
				valkeyErrors = errors.Join(valkeyErrors, err)
			}

			key := variableUpdate.VariableRef
			processLabel := func(labels []string) error {
				for _, label := range labels {
					value := alert.Labels[label]
					found, err := dataAdapter.DoVariableUpdateAndLog(
						ctx,
						variableUpdate,
						auditAdapter.CreateHubAction(value, variableUpdate, key, alert),
						key,
						label,
						value,
						int64(len(relevantLabels)),
					)
					if err != nil {
						return err
					}
					if !found {
						logger.Error("Unknown variable update operation",
							zap.String("operation", string(variableUpdate.Operation)),
							zap.Any("alert", alert),
						)
						continue
					}
				}
				return nil
			}

			switch variableUpdate.Operation {
			case datacore.VariableUpdateSetAddElement, datacore.VariableUpdateSetRemoveElement:
				valkeyErrors = errors.Join(valkeyErrors, processLabel(relevantLabels))
			default:
				if len(relevantLabels) > 1 {
					logger.Info("Multiple relevantLabels found for replace action",
						zap.String("selected_label", relevantLabels[0]),
						zap.Any("all_labels", relevantLabels),
					)
				}
				valkeyErrors = errors.Join(valkeyErrors, processLabel([]string{relevantLabels[0]}))
			}
		}

		if valkeyErrors != nil {
			logger.Error("Errors occurred writing updates to valkey", zap.Error(valkeyErrors))
			http.Error(w, "Handler can't successfully process the payload:\n "+valkeyErrors.Error(), http.StatusInternalServerError)
			return
		}

		logger.Info("Successfully wrote all variable updates")
		w.WriteHeader(http.StatusOK)
		_, _ = fmt.Fprintf(w, `{"success": "variable(s) updated"}`)
	}
}
