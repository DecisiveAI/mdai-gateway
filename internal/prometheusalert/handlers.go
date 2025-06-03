package prometheusalert

import (
	"encoding/json"
	"net/http"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/decisiveai/mdai-gateway/internal/utils"
	alertmanager "github.com/prometheus/alertmanager/template"
	"go.uber.org/zap"
)

func Handle(w http.ResponseWriter, body []byte, logger *zap.Logger, hub eventing.EventHubInterface) {
	var alertData alertmanager.Data
	if err := json.Unmarshal(body, &alertData); err != nil {
		utils.HandleHTTPError(w, logger, http.StatusBadRequest, "Invalid Prometheus alert format", err)
		return
	}

	logger.Info("Processing Prometheus alert", zap.Object("alert", loggedPrometheusAlert{alertData}))

	events := NewPrometheusAlertData(alertData).ToMdaiEvent(logger)

	var failedEventCount int
	for _, event := range events {
		if err := hub.PublishMessage(event); err != nil {
			failedEventCount++
			logger.Warn("Failed to publish event", zap.String("event_name", event.Name), zap.Error(err))
		}
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	response := prometheusAlertResponse{
		Message:    "Processed Prometheus alerts",
		Total:      len(events),
		Successful: len(events) - failedEventCount,
		Failed:     failedEventCount,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to write response", zap.Error(err))
	}
}
