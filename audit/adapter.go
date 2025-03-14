package audit

import (
	"context"
	"encoding/json"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"go.uber.org/multierr"
	"go.uber.org/zap"

	mdaiv1 "github.com/DecisiveAI/mdai-operator/api/v1"

	"github.com/decisiveai/event-handler-webservice/types"
	"github.com/valkey-io/valkey-go"
)

var (
	logger                  *zap.Logger
	valkeyAuditStreamExpiry = 30 * 24 * time.Hour
	metricRegex             = regexp.MustCompile(`([a-zA-Z_:][a-zA-Z0-9_:]*)\{`)
)

const (
	HubName          = "hub_name"
	Expression       = "expression"
	CurrentValue     = "current_value"
	AlertName        = "alert_name"
	EventTriggered   = "event_triggered"
	VariableUpdated  = "variable_updated"
	CollectorRestart = "collector_restart"
	ValkeyUpdate     = "valkey_update"
	Evaluation       = "evaluation"

	mdaiHubEventHistoryStreamName = "mdai_hub_event_history"
)

func init() {
	var err error
	logger, err = zap.NewProduction()
	if err != nil {
		panic("failed to initialize logger: " + err.Error())
	}
}

func HandleEventsGet(ctx context.Context, valkeyClient valkey.Client) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		result := valkeyClient.Do(ctx, valkeyClient.B().Xrevrange().Key(mdaiHubEventHistoryStreamName).End("+").Start("-").Build())
		if err := result.Error(); err != nil {
			logger.Error("valkey error", zap.Error(err))
			http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
			return
		}

		resultList, err := result.ToArray()
		if err != nil {
			logger.Error("failed to get valkey variable as map", zap.Error(err))
			http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
			return
		}

		entries := make([]map[string]any, 0)
		for _, entry := range resultList {
			entryMap, err := entry.AsXRangeEntry()
			if err != nil {
				logger.Error("failed to convert entry to map", zap.Error(err))
				continue
			}

			if processedEntry := processEntry(entryMap); processedEntry != nil {
				entries = append(entries, processedEntry)
			}
		}

		resultMapJson, err := json.Marshal(entries)
		if err != nil {
			logger.Error("failed to marshal events map to json", zap.Error(err))
			http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)
		if _, err := w.Write(resultMapJson); err != nil {
			logger.Error("Failed to write response body (y tho)", zap.Error(err))
		}
	}
}

func processEntry(entryMap valkey.XRangeEntry) map[string]any {
	timestamp := entryMap.FieldValues["timestamp"]
	hubName := entryMap.FieldValues["hub_name"]
	eventType := entryMap.FieldValues["type"]

	switch eventType {
	case CollectorRestart:
		storedVars := showHubCollectorRestartVariables(entryMap.FieldValues)
		return map[string]any{
			"timestamp": timestamp,
			"hubName":   hubName,
			"event":     "mdai/" + CollectorRestart,
			"trigger":   "mdai/" + ValkeyUpdate,
			"context": map[string]any{
				"storedVariables": storedVars,
			},
		}
	case VariableUpdated:
		return map[string]any{
			"timestamp": timestamp,
			"hubName":   hubName,
			"event":     "action/" + VariableUpdated,
			"trigger":   entryMap.FieldValues["event"] + "/" + entryMap.FieldValues["status"],
			"context": map[string]any{
				"variableRef": entryMap.FieldValues["variable_ref"],
				"operation":   entryMap.FieldValues["operation"],
				"target":      entryMap.FieldValues["target"],
			},
			"payload": map[string]any{
				"variable": entryMap.FieldValues["variable"],
			},
		}
	case EventTriggered:
		return map[string]any{
			"timestamp": timestamp,
			"hubName":   hubName,
			"event":     Evaluation + "/prometheus_alert",
			"trigger":   Evaluation,
			"context": map[string]any{
				"name":       entryMap.FieldValues["name"],
				"expression": entryMap.FieldValues["expression"],
				"metric":     entryMap.FieldValues["metric_name"],
			},
			"payload": map[string]any{
				"status":              entryMap.FieldValues["status"],
				"relevantLabelValues": entryMap.FieldValues["relevant_label_values"],
				"value":               entryMap.FieldValues["value"],
			},
		}
	default:
		transformedEntry := make(map[string]any)
		for k, v := range entryMap.FieldValues {
			transformedEntry[k] = v
		}
		return transformedEntry
	}
}

func showHubCollectorRestartVariables(fields map[string]string) string {
	var storedVars []string
	ignoreKeys := map[string]bool{
		"timestamp": true,
		"type":      true,
	}
	for key, value := range fields {
		if strings.HasSuffix(key, "_CSV") && !ignoreKeys[key] && value != "" && value != "n/a" {
			storedVars = append(storedVars, value)
		}
	}
	return strings.Join(storedVars, ",")
}

func DoVariableUpdateAndLog(ctx context.Context, valkeyClient valkey.Client, variableUpdateCommand valkey.Completed, mdaiHubAction MdaiHubAction, valkeyKey string) error {
	logger.Info("Performing "+mdaiHubAction.Operation+" operation",
		zap.String("variable", valkeyKey),
		zap.Any("mdaiHubAction", mdaiHubAction),
	)
	auditLogCommand := makeAuditLogActionCommand(valkeyClient, mdaiHubAction)
	results := valkeyClient.DoMulti(ctx,
		variableUpdateCommand,
		auditLogCommand,
	)
	valkeyMultiErr := accumulateValkeyErrors(results)
	return valkeyMultiErr
}

func makeAuditLogActionCommand(valkeyClient valkey.Client, mdaiHubAction MdaiHubAction) valkey.Completed {
	return valkeyClient.B().Xadd().Key(mdaiHubEventHistoryStreamName).Minid().Threshold(getAuditLogTTLMinId()).Id("*").FieldValue().FieldValueIter(mdaiHubAction.ToSequence()).Build()
}

func InsertAuditLogEvent(ctx context.Context, valkeyClient valkey.Client, mdaiHubEvent MdaiHubEvent) error {
	result := valkeyClient.Do(ctx, valkeyClient.B().Xadd().Key(mdaiHubEventHistoryStreamName).Minid().Threshold(getAuditLogTTLMinId()).Id("*").FieldValue().FieldValueIter(mdaiHubEvent.ToSequence()).Build())
	if err := result.Error(); err != nil {
		logger.Error("Valkey error", zap.Error(err))
		return err
	}
	return nil
}

func CreateHubEvent(relevantLabels []string, alert types.Alert) MdaiHubEvent {
	metricMatch := metricRegex.FindStringSubmatch(alert.Annotations[Expression])
	metricName := ""
	if len(metricMatch) > 1 {
		metricName = metricMatch[1]
	}

	relevantLabelValues := make([]string, len(relevantLabels))
	for idx, relevantLabel := range relevantLabels {
		relevantLabelValues[idx] = alert.Labels[relevantLabel]
	}

	mdaiHubEvent := MdaiHubEvent{
		HubName:             alert.Annotations[HubName],
		Name:                alert.Annotations[AlertName],
		RelevantLabelValues: strings.Join(relevantLabelValues, ","),
		Type:                EventTriggered,
		MetricName:          metricName,
		Expression:          alert.Annotations[Expression],
		Value:               alert.Annotations[CurrentValue],
		Status:              alert.Status,
	}
	return mdaiHubEvent
}

func CreateHubAction(relevantLabels []string, variableUpdate *mdaiv1.VariableUpdate, valkeyKey string, alert types.Alert) MdaiHubAction {
	mdaiHubAction := MdaiHubAction{
		HubName:     alert.Annotations[HubName],
		Event:       alert.Annotations[AlertName],
		Status:      alert.Status,
		Type:        VariableUpdated,
		Operation:   variableUpdate.Operation,
		Target:      valkeyKey,
		VariableRef: variableUpdate.VariableRef,
		Variable:    alert.Labels[relevantLabels[0]],
	}
	return mdaiHubAction
}

func getAuditLogTTLMinId() string {
	minid := strconv.FormatInt(time.Now().Add(-valkeyAuditStreamExpiry).UnixMilli(), 10)
	return minid
}

func accumulateValkeyErrors(results []valkey.ValkeyResult) error {
	var valkeyMultiErr error
	for _, result := range results {
		valkeyError := result.Error()
		if valkeyError != nil {
			valkeyMultiErr = multierr.Append(valkeyMultiErr, valkeyError)
			logger.Error("Valkey error", zap.Error(valkeyError))
		}
	}
	return valkeyMultiErr
}
