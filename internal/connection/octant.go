package connection

import (
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type DeploymentType string

// TODO: Refactor connection operations to use tasksets/plans instead of if-argo-then
//type DeploymentTask func(ctx context.Context, name string, namespace string, connection OctantConnectionData) (any, error)
//type DeploymentTaskSet map[string][]DeploymentTask

var (
	ArgoForceSyncDeploymentType DeploymentType = "argocd-force-sync"
)

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

var _ Connection[OctantConnectionData] = (*OctantConnection)(nil)

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
	//taskSets      map[DeploymentType]DeploymentTaskSet
}

func NewOctantConnection(httpClient *http.Client, k8sClient kubernetes.Interface) *OctantConnection {
	// TODO: Refactor connection operations to use tasksets/plans instead of if-argo-then
	//taskSets := map[DeploymentType]DeploymentTaskSet{
	//	ArgoForceSyncDeploymentType: {
	//		"GET": ...,
	//		"POST": ...,
	//		"DELETE": ...,
	//	},
	//}
	return &OctantConnection{
		httpClient: httpClient,
		k8sClient:  k8sClient,
		argoClient: &integration.ArgoCDIntegration{
			K8sClient: k8sClient,
		},
		datadogClient: &integration.DataDogIntegration{
			K8sClient: k8sClient,
		},
	}
}

func (oc *OctantConnection) GetConnectionByName(ctx context.Context, namespace, name string) (*OctantConnectionData, error) {
	configmap, err := oc.k8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil // nolint: nilnil
		}
		return nil, fmt.Errorf("failed to get configmap %s: %w", connectionsConfigmapName, err)
	}

	if _, ok := configmap.Data[name]; !ok {
		return nil, nil // nolint: nilnil
	}

	var connection OctantConnectionData
	if err = json.Unmarshal([]byte(configmap.Data[name]), &connection); err != nil {
		return nil, fmt.Errorf("failed to unmarshal connection data: %w", err)
	}

	// TODO: This should be refactored to a more robust deployment-based task system
	if connection.Deployment != nil && connection.Deployment.Type == ArgoForceSyncDeploymentType {
		argoApp, err := oc.getArgoAppStatus(ctx, name, namespace, connection)
		if err != nil {
			return &connection, err
		}

		connection.Status = argoApp
	}

	return &connection, nil
}

func (oc *OctantConnection) SaveConnection(ctx context.Context, connection OctantConnectionData, namespace, connectionName string) error {
	jsonData, err := json.Marshal(connection)
	if err != nil {
		return fmt.Errorf("failed to marshal connection data: %w", err)
	}

	cm, err := oc.k8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			// Create the confmap if it does not exist
			return createConnectionConfigMap(ctx, oc.k8sClient, namespace, connectionsConfigmapName, connectionName, string(jsonData))
		}
		return fmt.Errorf("failed to fetch configmap %s: %w", connectionsConfigmapName, err)
	}
	// Update the confmap if it already exists
	updateConfigMapErr := updateConfigMapWithConnection(ctx, oc.k8sClient, namespace, cm, connectionName, string(jsonData))
	if updateConfigMapErr != nil {
		return updateConfigMapErr
	}

	// TODO: This should be refactored to a more robust deployment-based task system
	if connection.Deployment != nil && connection.Deployment.Type == ArgoForceSyncDeploymentType {
		err := oc.pushArgoApp(ctx, namespace, connectionName, connection)
		if err != nil {
			return err
		}
	}

	return nil
}

func (oc *OctantConnection) DeleteConnection(ctx context.Context, namespace, connectionName string) error {
	cm, err := oc.k8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to fetch configmap %s: %w", connectionsConfigmapName, err)
	}

	var connection OctantConnectionData
	if err = json.Unmarshal([]byte(cm.Data[connectionName]), &connection); err != nil {
		return fmt.Errorf("failed to unmarshal connection data: %w", err)
	}

	if cm.Data == nil {
		return nil
	}
	if _, exists := cm.Data[connectionName]; !exists {
		return nil
	}

	// TODO: This should be refactored to a more robust deployment-based task system
	if connection.Deployment != nil && connection.Deployment.Type == ArgoForceSyncDeploymentType {
		if err := oc.deleteArgoApp(ctx, connectionName, namespace, connection); err != nil {
			return err
		}
	}

	delete(cm.Data, connectionName)

	if _, err = oc.k8sClient.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update configmap %s after deletion: %w", connectionsConfigmapName, err)
	}

	return nil
}
