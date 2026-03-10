package server

import (
	"context"
	"github.com/decisiveai/mdai-gateway/internal/integration"
	"k8s.io/client-go/kubernetes"
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
	K8sClient           kubernetes.Interface
	K8sNamespace        string
}

func NewRouter(ctx context.Context, deps HandlerDeps) *http.ServeMux {
	mainRouter := http.NewServeMux()

	mainRouter.HandleFunc("GET /audit", handleAuditEventsGet(ctx, deps))
	mainRouter.Handle("POST /alerts/alertmanager", requireJSON(handlePromAlertsPost(deps)))
	mainRouter.Handle("GET /variables/list", handleListAllVariables(ctx, deps))
	mainRouter.Handle("GET /variables/list/hub/{hubName}", handleListHubVariables(ctx, deps))
	mainRouter.Handle("GET /variables/values/hub/{hubName}/var/{varName}", handleGetVariables(ctx, deps))
	mainRouter.Handle("POST /variables/hub/{hubName}/var/{varName}", handleSetDeleteVariables(ctx, deps))
	mainRouter.Handle("DELETE /variables/hub/{hubName}/var/{varName}", handleSetDeleteVariables(ctx, deps))
	mainRouter.Handle("POST /opamp", deps.OpAMPServer.HandlerFunc)

	integrationsHandler := NewDatadogHandler(&integration.DataDogIntegration{
		K8sClient: deps.K8sClient,
	})

	datadogRouter := http.NewServeMux()
	mainRouter.Handle("GET /", integrationsHandler.GetIntegrations(ctx, deps))
	mainRouter.Handle("PUT /{integrationName}", integrationsHandler.PutIntegrationData(ctx, deps))
	mainRouter.Handle("DELETE /{integrationName}", integrationsHandler.DeleteIntegration(ctx, deps))

	mainRouter.Handle("/integrations/datadog", datadogRouter)

	return mainRouter
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
