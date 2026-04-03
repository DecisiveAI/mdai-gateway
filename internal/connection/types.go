package connection

import (
	"github.com/mydecisive/mdai-gateway/internal/metrics"
	"github.com/mydecisive/mdai-gateway/internal/telemetry"
	"go.uber.org/zap"
	"net/http"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"k8s.io/client-go/kubernetes"
)

//revive:disable:max-public-structs

type DeploymentType string

// TODO: Refactor connection operations to use tasksets/plans instead of if-argo-then
// type DeploymentTask func(ctx context.Context, name string, namespace string, connection OctantConnectionData) (any, error)
// type DeploymentTaskSet map[string][]DeploymentTask.

const (
	ArgoSideloadDeploymentType  DeploymentType = "argocd-sideload"
	ArgoManifestsDeploymentType DeploymentType = "argocd-manifests"
)

type OctantConnectionDestination struct {
	DestinationType string `json:"type"`
	IntegrationName string `json:"integrationName"`
}

type OctantConnectionData struct {
	SourceType     string                        `json:"sourceType"`
	Destinations   []OctantConnectionDestination `json:"destinations"`
	TelemetryTypes []telemetry.MLT               `json:"telemetryTypes"`
	Deployment     *Deployment                   `json:"deployment,omitempty"`
	Status         any                           `json:"status,omitempty"`
}

type Deployment struct {
	Type            DeploymentType `json:"type"`
	IntegrationName string         `json:"integrationName"`
}

type OctantConnection struct {
	httpClient        *http.Client
	k8sClient         kubernetes.Interface
	argoClient        integration.Integration[integration.ArgoCDIntegrationData]
	datadogClient     integration.Integration[integration.DataDogIntegrationData]
	PrometheusClient  promv1.API
	logger            *zap.Logger
	connectionMetrics *metrics.ConnectionStatus
	// TODO: Refactor connection operations to use tasksets/plans instead of if-argo-then
	// taskSets      map[DeploymentType]DeploymentTaskSet
}

func NewOctantConnection(httpClient *http.Client, k8sClient kubernetes.Interface, argoClient integration.Integration[integration.ArgoCDIntegrationData], datadogClient integration.Integration[integration.DataDogIntegrationData], promClient promv1.API, logger *zap.Logger) *OctantConnection {
	return &OctantConnection{
		httpClient:        httpClient,
		k8sClient:         k8sClient,
		argoClient:        argoClient,
		datadogClient:     datadogClient,
		logger:            logger,
		connectionMetrics: metrics.NewConnectionStatus(promClient, logger),
	}
}
