package connection

import (
	"context"
	"net/http"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	"k8s.io/client-go/kubernetes"
)

//revive:disable:max-public-structs

type DeploymentType string

// TODO: Refactor connection operations to use tasksets/plans instead of if-argo-then
// type DeploymentTask func(ctx context.Context, name string, namespace string, connection OctantConnectionData) (any, error)
// type DeploymentTaskSet map[string][]DeploymentTask.

const ArgoForceSyncDeploymentType DeploymentType = "argocd-force-sync"

type OctantConnectionDestination struct {
	DestinationType string `json:"type"`
	IntegrationName string `json:"integrationName"`
}

type OctantConnectionData struct {
	SourceType     string                        `json:"sourceType"`
	Destinations   []OctantConnectionDestination `json:"destinations"`
	TelemetryTypes []Telemetry                   `json:"telemetryTypes"`
	Deployment     *Deployment                   `json:"deployment,omitempty"`
	Status         any                           `json:"status,omitempty"`
}

type Deployment struct {
	Type            DeploymentType `json:"type"`
	IntegrationName string         `json:"integrationName"`
}

type ArgoIntegrationClient interface {
	GetIntegrationByName(ctx context.Context, namespace, name string) (*integration.ArgoCDIntegrationData, error)
}

type DatadogIntegrationClient interface {
	GetIntegrationByName(ctx context.Context, namespace, name string) (*integration.DataDogIntegrationData, error)
}
type OctantConnection struct {
	httpClient    *http.Client
	k8sClient     kubernetes.Interface
	argoClient    ArgoIntegrationClient
	datadogClient DatadogIntegrationClient
	// TODO: Refactor connection operations to use tasksets/plans instead of if-argo-then
	// taskSets      map[DeploymentType]DeploymentTaskSet
}
