package connection

import (
	"context"
	_ "embed" // nolint: revive
	"encoding/json"
	"fmt"
	"net/http"
	"slices"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

var _ Connection[OctantConnectionData] = (*OctantConnection)(nil)

func NewOctantConnection(httpClient *http.Client, k8sClient kubernetes.Interface) *OctantConnection {
	// TODO: Refactor connection operations to use tasksets/plans instead of if-argo-then
	// taskSets := map[DeploymentType]DeploymentTaskSet{
	//	ArgoForceSyncDeploymentType: {
	//		"GET": ...,
	//		"POST": ...,
	//		"DELETE": ...,
	//	},
	// }
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
	if connection.Deployment != nil && connection.Deployment.Type == ArgoSideloadDeploymentType {
		argoApp, err := oc.getArgoAppStatus(ctx, name, namespace, connection)
		if err != nil {
			return &connection, err
		}

		connection.Status = argoApp
	}

	return &connection, nil
}

func (oc *OctantConnection) SaveConnection(ctx context.Context, connection OctantConnectionData, namespace, connectionName string) error {
	if !slices.Contains(([]DeploymentType{ArgoManifestsDeploymentType, ArgoSideloadDeploymentType}), connection.Deployment.Type) {
		return fmt.Errorf("invalid deployment type: %s", connection.Deployment.Type)
	}
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
	if connection.Deployment != nil && connection.Deployment.Type == ArgoSideloadDeploymentType {
		err := oc.pushArgoApp(ctx, namespace, connectionName, connection)
		if err != nil {
			return err
		}
	}

	return nil
}

func (oc *OctantConnection) DeleteConnection(ctx context.Context, namespace, connectionName string) error {
	cm, getCMErr := oc.k8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if getCMErr != nil {
		if k8serrors.IsNotFound(getCMErr) {
			return nil
		}
		return fmt.Errorf("failed to fetch configmap %s: %w", connectionsConfigmapName, getCMErr)
	}

	if cm.Data == nil {
		return nil
	}
	if _, exists := cm.Data[connectionName]; !exists {
		return nil
	}

	var connection OctantConnectionData
	if err := json.Unmarshal([]byte(cm.Data[connectionName]), &connection); err != nil {
		return fmt.Errorf("failed to unmarshal connection data: %w", err)
	}

	// TODO: This should be refactored to a more robust deployment-based task system
	if connection.Deployment != nil && connection.Deployment.Type == ArgoSideloadDeploymentType {
		if deleteErr := oc.deleteArgoApp(ctx, connectionName, namespace, connection); deleteErr != nil {
			return deleteErr
		}
	}

	delete(cm.Data, connectionName)

	if _, err := oc.k8sClient.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{}); err != nil {
		return fmt.Errorf("failed to update configmap %s after deletion: %w", connectionsConfigmapName, err)
	}

	return nil
}
