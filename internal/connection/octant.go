package connection

import (
	"context"
	"encoding/json"
	"fmt"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

type OctantConnectionData struct {
	SourceType     string      `json:"sourceType"`
	TelemetryTypes []Telemetry `json:"telemetryTypes"`
	Deployment     *Deployment `json:"deployment"`
}

type Deployment struct {
	Type   string         `json:"type"`
	Fields map[string]any `json:"fields"`
}

type ArgoDeployment struct {
	Branch string `json:"branch"`
}

var _ Connection[OctantConnectionData] = (*OctantConnection)(nil)

type OctantConnection struct {
	K8sClient kubernetes.Interface
}

func (oc *OctantConnection) GetConnectionByName(ctx context.Context, namespace, name string) (*OctantConnectionData, error) {
	configmap, err := oc.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil // nolint: nilnil
		}
		return nil, fmt.Errorf("failed to get configmap %s: %w", connectionsConfigmapName, err)
	}

	if _, ok := configmap.Data[name]; !ok {
		return nil, fmt.Errorf("connection '%s' not found", name)
	}

	var connection OctantConnectionData
	if unmarshalErr := json.Unmarshal([]byte(configmap.Data[name]), &connection); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to unmarshal connection data: %w", unmarshalErr)
	}
	return &connection, nil
}

func (oc *OctantConnection) SaveConnection(ctx context.Context, connection OctantConnectionData, namespace, connectionName string) error {
	jsonData, err := json.Marshal(connection)
	if err != nil {
		return fmt.Errorf("failed to marshal connection data: %w", err)
	}

	cm, err := oc.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	isNotFound := k8serrors.IsNotFound(err)
	if err != nil && !isNotFound {
		return fmt.Errorf("failed to fetch configmap %s: %w", connectionsConfigmapName, err)
	}

	if isNotFound {
		// Create the confmap if it does not exist
		return createConnectionConfigMap(ctx, oc.K8sClient, namespace, connectionsConfigmapName, connectionName, string(jsonData))
	}
	// Update the secret if it already exists
	return updateConfigMapWithConnection(ctx, oc.K8sClient, namespace, cm, connectionsConfigmapName, string(jsonData))
}

func (oc *OctantConnection) DeleteConnection(ctx context.Context, namespace, connectionName string) error {
	cm, err := oc.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to fetch configmap %s: %w", connectionsConfigmapName, err)
	}

	if cm.Data == nil {
		return nil
	}
	if _, exists := cm.Data[connectionName]; !exists {
		return nil
	}

	delete(cm.Data, connectionName)

	_, err = oc.K8sClient.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update configmap %s after deletion: %w", connectionsConfigmapName, err)
	}

	return nil
}
