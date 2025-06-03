package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/decisiveai/mdai-gateway/internal/mdaievent"
	"github.com/decisiveai/mdai-gateway/internal/prometheusalert"
	"github.com/decisiveai/mdai-gateway/internal/utils"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type loggableMapStringAny map[string]any

func (lmsa loggableMapStringAny) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	for k, v := range lmsa {
		switch v := v.(type) {
		case string:
			enc.AddString(k, v)
		case int:
			enc.AddInt(k, v)
		case bool:
			enc.AddBool(k, v)
		case map[string]any:
			_ = enc.AddObject(k, loggableMapStringAny(v))
		default:
			enc.AddString(k, fmt.Sprintf("%v", v))
		}
	}
	return nil
}

func handlePostRequest(_ context.Context, w http.ResponseWriter, r *http.Request, hub eventing.EventHubInterface, logger *zap.Logger) {
	body, err := io.ReadAll(r.Body)
	if err != nil {
		utils.HandleHTTPError(w, logger, http.StatusBadRequest, "Failed to read request body", err)
		return
	}

	logger.Debug("Received POST request body", zap.ByteString("body", body))

	// Try to determine the payload type by checking for key fields
	var raw map[string]any
	if err := json.Unmarshal(body, &raw); err != nil {
		utils.HandleHTTPError(w, logger, http.StatusBadRequest, "Invalid JSON in request body", err)
		return
	}

	switch {
	case isPrometheusAlert(raw):
		prometheusalert.Handle(w, body, logger, hub)
	case isMdaiEvent(raw):
		mdaievent.Handle(w, body, logger, hub)
	default:
		logger.Warn("Unrecognized payload format", zap.Object("raw_json", loggableMapStringAny(raw)))
		http.Error(w, "Invalid request body format", http.StatusBadRequest)
		return
	}
}

func isPrometheusAlert(raw map[string]any) bool {
	_, hasReceiver := raw["receiver"]
	_, hasAlerts := raw["alerts"]
	return hasReceiver && hasAlerts
}

func isMdaiEvent(raw map[string]any) bool {
	_, hasName := raw["name"]
	_, hasPayload := raw["payload"]
	return hasName && hasPayload
}
