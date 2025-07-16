package server

import (
	"context"
	"github.com/decisiveai/mdai-data-core/audit"
	"net/http"

	datacorekube "github.com/decisiveai/mdai-data-core/kube"
	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

type HandlerDeps struct {
	Logger              *zap.Logger
	ValkeyClient        valkey.Client
	AuditAdapter        *audit.AuditAdapter
	EventPublisher      eventing.Publisher
	ConfigMapController *datacorekube.ConfigMapController
}

func NewRouter(ctx context.Context, deps HandlerDeps) *http.ServeMux {
	router := http.NewServeMux()

	router.HandleFunc("GET /events", handleEventsGet(ctx, deps))
	router.HandleFunc("POST /events", handleEventsPost(ctx, deps))
	router.Handle("GET /variables/list/", handleListAllVariables(ctx, deps))
	router.Handle("GET /variables/list/hub/{hubName}/", handleListHubVariables(ctx, deps))
	router.Handle("GET /variables/values/hub/{hubName}/var/{varName}/", handleGetVariables(ctx, deps))
	router.Handle("POST /variables/hub/{hubName}/var/{varName}/", handleSetDeleteVariables(ctx, deps))
	router.Handle("DELETE /variables/hub/{hubName}/var/{varName}/", handleSetDeleteVariables(ctx, deps))

	return router
}
