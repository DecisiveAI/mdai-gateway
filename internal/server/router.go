package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/mydecisive/mdai-data-core/audit"
	"github.com/mydecisive/mdai-data-core/eventing/publisher"
	datacorekube "github.com/mydecisive/mdai-data-core/kube"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	"github.com/mydecisive/mdai-gateway/internal/connection"
	"github.com/mydecisive/mdai-gateway/internal/integration"
	"github.com/mydecisive/mdai-gateway/internal/opamp"
	gatewayvalkey "github.com/mydecisive/mdai-gateway/internal/valkey"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
)

type HandlerDeps struct {
	Logger                 *zap.Logger
	ValkeyClient           valkey.Client
	VariableReader         *gatewayvalkey.Reader
	SlowValueReadThreshold time.Duration
	AuditAdapter           *audit.AuditAdapter
	EventPublisher         publisher.Publisher
	ConfigMapController    *datacorekube.ConfigMapController
	Deduper                *adapter.Deduper
	OpAMPServer            *opamp.OpAMPControlServer
	K8sClient              kubernetes.Interface
	K8sNamespace           string
	HTTPClient             *http.Client
}

func NewRouter(ctx context.Context, deps HandlerDeps) *http.ServeMux {
	mainRouter := http.NewServeMux()

	mainRouter.HandleFunc("GET /audit", handleAuditEventsGet(ctx, deps))
	mainRouter.Handle("POST /alerts/alertmanager", requireJSON(handlePromAlertsPost(deps)))

	mainRouter.Handle("GET /variables/list", handleListAllVariables(ctx, deps))
	mainRouter.Handle("GET /variables/list/hub/{hubName}", handleListHubVariables(ctx, deps))
	mainRouter.Handle("GET /variables/values/hub/{hubName}", handleGetHubVariableValues(ctx, deps))
	mainRouter.Handle("GET /variables/values/hub/{hubName}/var/{varName}", handleGetVariables(ctx, deps))
	// write operations are allowed only for manual variables
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
	datadogRouter.Handle("GET /", ddIntegrationHandler.GetIntegrations(ctx))
	datadogRouter.Handle("PUT /{integrationName}", ddIntegrationHandler.PutIntegrationData(ctx))
	datadogRouter.Handle("DELETE /{integrationName}", ddIntegrationHandler.DeleteIntegration(ctx))

	argocdRouter := http.NewServeMux()
	argocdRouter.Handle("GET /", argocdIntegrationHandler.GetIntegrations(ctx))
	argocdRouter.Handle("PUT /{integrationName}", argocdIntegrationHandler.PutIntegrationData(ctx))
	argocdRouter.Handle("DELETE /{integrationName}", argocdIntegrationHandler.DeleteIntegration(ctx))

	mainRouter.Handle("/integrations/datadog/", http.StripPrefix("/integrations/datadog", datadogRouter))
	mainRouter.Handle("/integrations/argocd/", http.StripPrefix("/integrations/argocd", argocdRouter))

	connectionsHandler := NewConnectionsHandler(connection.NewOctantConnection(deps.HTTPClient, deps.K8sClient), deps.K8sNamespace, deps.Logger)

	connectionsRouter := http.NewServeMux()
	connectionsRouter.Handle("GET /{connectionName}", connectionsHandler.GetConnectionByName(ctx))
	connectionsRouter.Handle("PUT /{connectionName}", connectionsHandler.SaveConnectionData(ctx))
	connectionsRouter.Handle("POST /{connectionName}/manifests/{format}", connectionsHandler.GenerateManifestsForGivenConnection())
	connectionsRouter.Handle("DELETE /{connectionName}", connectionsHandler.DeleteConnectionByName(ctx))

	mainRouter.Handle("/connections/", http.StripPrefix("/connections", connectionsRouter))

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
