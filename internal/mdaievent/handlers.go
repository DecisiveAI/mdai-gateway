package mdaievent

import (
	"encoding/json"
	"net/http"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/decisiveai/mdai-gateway/internal/utils"
	"go.uber.org/zap"
)

func Handle(w http.ResponseWriter, body []byte, logger *zap.Logger, hub eventing.EventHubInterface) {
	var event eventing.MdaiEvent
	if err := json.Unmarshal(body, &event); err != nil {
		utils.HandleHTTPError(w, logger, http.StatusBadRequest, "Invalid MdaiEvent format", err)
		return
	}

	event.ApplyDefaults()
	if err := event.Validate(); err != nil {
		utils.HandleHTTPError(w, logger, http.StatusBadRequest, err.Error(), nil)
		return
	}

	logger.Info("Processing MdaiEvent", zap.Object("event", event))

	if err := hub.PublishMessage(event); err != nil {
		utils.HandleHTTPError(w, logger, http.StatusInternalServerError, "Failed to publish MdaiEvent", err)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)

	response := mdaiEventResponse{
		Message: "Event received successfully",
		Id:      event.Id,
	}

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("Failed to write response", zap.Error(err))
	}
}
