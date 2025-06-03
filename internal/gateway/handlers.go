package gateway

import (
	"context"
	"net/http"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/decisiveai/mdai-gateway/internal/utils"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type loggedRequest struct {
	*http.Request
}

func (lr loggedRequest) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	enc.AddString("method", lr.Method)
	enc.AddString("url", lr.URL.String())
	enc.AddString("remote_addr", lr.RemoteAddr)
	enc.AddString("user_agent", lr.UserAgent())
	enc.AddString("host", lr.Host)
	enc.AddString("request_uri", lr.RequestURI)
	return nil
}

func HandleEventsRoute(ctx context.Context, valkeyClient valkey.Client, hub eventing.EventHubInterface, logger *zap.Logger) http.HandlerFunc {
	valkeyAuditStreamExpiry := utils.GetValkeyExpiry(logger)

	return func(w http.ResponseWriter, r *http.Request) {
		logger.Info("handling request", zap.Object("request", loggedRequest{r}))

		switch r.Method {
		case http.MethodGet:
			handleGetRequest(ctx, w, valkeyClient, valkeyAuditStreamExpiry, logger)
		case http.MethodPost:
			handlePostRequest(ctx, w, r, hub, logger)
		default:
			utils.HandleHTTPError(w, logger, http.StatusMethodNotAllowed, "Method not allowed", nil)
		}
	}
}
