package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/decisiveai/mdai-data-core/eventing/publisher"
	datacorekube "github.com/decisiveai/mdai-data-core/kube"
	"github.com/decisiveai/mdai-gateway/internal/adapter"
	"github.com/decisiveai/mdai-gateway/internal/opamp"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type HandlerDeps struct {
	Logger              *zap.Logger
	ValkeyClient        valkey.Client
	AuditAdapter        *audit.AuditAdapter
	EventPublisher      publisher.Publisher
	ConfigMapController *datacorekube.ConfigMapController
	Deduper             *adapter.Deduper
	OpAMPServer         *opamp.OpAMPControlServer
}

func NewRouter(ctx context.Context, deps HandlerDeps) *http.ServeMux {
	router := http.NewServeMux()

	router.HandleFunc("GET /audit", handleAuditEventsGet(ctx, deps))
	router.Handle("POST /alerts/alertmanager", requireJSON(handlePromAlertsPost(deps)))
	router.Handle("GET /variables/list", handleListAllVariables(ctx, deps))
	router.Handle("GET /variables/list/hub/{hubName}", handleListHubVariables(ctx, deps))
	router.Handle("GET /variables/values/hub/{hubName}/var/{varName}", handleGetVariables(ctx, deps))
	router.Handle("POST /variables/hub/{hubName}/var/{varName}", handleSetDeleteVariables(ctx, deps))
	router.Handle("DELETE /variables/hub/{hubName}/var/{varName}", handleSetDeleteVariables(ctx, deps))
	router.Handle("GET /opamp", deps.OpAMPServer.HandlerFunc)

	return router
}

func requireJSON(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ct := r.Header.Get("Content-Type"); !strings.HasPrefix(ct, "application/json") {
			http.Error(w, "Content-Type header must be application/json", http.StatusUnsupportedMediaType)
			return
		}

		next.ServeHTTP(w, r)
	})
}
