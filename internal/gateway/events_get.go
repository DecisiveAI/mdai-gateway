package gateway

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/go-logr/zapr"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func handleGetRequest(ctx context.Context, w http.ResponseWriter, valkeyClient valkey.Client, valkeyAuditStreamExpiry time.Duration, logger *zap.Logger) {
	auditAdapter := audit.NewAuditAdapter(zapr.NewLogger(logger), valkeyClient, valkeyAuditStreamExpiry)
	eventsMap, err := auditAdapter.HandleEventsGet(ctx)
	if err != nil {
		logger.Error("failed to get events", zap.Error(err))
		http.Error(w, "Unable to fetch history from Valkey", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(eventsMap); err != nil {
		logger.Error("Failed to write response", zap.Error(err))
	}
}
