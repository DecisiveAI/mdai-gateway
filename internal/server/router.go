package server

import (
	"context"
	"net/http"
	"strings"

	"github.com/mydecisive/mdai-data-core/audit"
	"github.com/mydecisive/mdai-data-core/eventing/publisher"
	datacorekube "github.com/mydecisive/mdai-data-core/kube"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	"github.com/mydecisive/mdai-gateway/internal/integration"
	"github.com/mydecisive/mdai-gateway/internal/opamp"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
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

	ddIntegrationHandler := NewDatadogHandler(&integration.DataDogIntegration{
		K8sClient: deps.K8sClient,
	}, deps.K8sNamespace, deps.Logger)
	argocdIntegrationHandler := NewArgoCDHandler(&integration.ArgoCDIntegration{
		K8sClient: deps.K8sClient,
	}, deps.K8sNamespace, deps.Logger)

	datadogRouter := http.NewServeMux()
	datadogRouter.Handle("GET /integrations/datadog", ddIntegrationHandler.GetIntegrations(ctx))
	datadogRouter.Handle("PUT /integrations/datadog/{integrationName}", ddIntegrationHandler.PutIntegrationData(ctx))
	datadogRouter.Handle("DELETE /integrations/datadog/{integrationName}", ddIntegrationHandler.DeleteIntegration(ctx))

	argocdRouter := http.NewServeMux()
	argocdRouter.Handle("GET /integrations/argocd", argocdIntegrationHandler.GetIntegrations(ctx))
	argocdRouter.Handle("PUT /integrations/argocd/{integrationName}", argocdIntegrationHandler.PutIntegrationData(ctx))
	argocdRouter.Handle("DELETE /integrations/argocd/{integrationName}", argocdIntegrationHandler.DeleteIntegration(ctx))

	mainRouter.Handle("/integrations/datadog", datadogRouter)
	mainRouter.Handle("/integrations/argocd", argocdRouter)

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
