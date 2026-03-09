package integration

import (
	"context"
	"encoding/json"
	"fmt"
	corev1 "k8s.io/api/core/v1"
	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
)

const DataDogIntegrationType Type = "datadog"

type DataDogIntegrationData struct {
	ApiKey string `json:"api_key"`
	DDUrl  string `json:"dd_url"`
}

type DataDogIntegration struct {
	K8sClient kubernetes.Interface
}

var _ Integration = (*DataDogIntegration)(nil)

// GetIntegrationsFromSecret TODO
func (ddi *DataDogIntegration) GetIntegrationsFromSecret(ctx context.Context, namespace string) (map[string]any, error) {
	secret, err := ddi.k8sClient.CoreV1().Secrets(namespace).Get(ctx, "mdai-datadog-integration", metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to get secret %s: %w", "mdai-datadog-integration", err)
	}

	integrations := make(map[string]any)
	for name, data := range secret.Data {
		var payload any
		if err := json.Unmarshal(data, &payload); err != nil {
			continue // Skip invalid JSON entries
		}
		integrations[name] = payload
	}

	return integrations, nil
}

// SetIntegration adds or updates a named integration payload in the specified integration type's secret. T represents the integration model (e.g., DataDogIntegration).
func (ddi *DataDogIntegration) SetIntegration(ctx context.Context, namespace, integrationName string, integrationData any) error {
	jsonData, err := json.Marshal(integrationData)
	if err != nil {
		return fmt.Errorf("failed to marshal integration data: %w", err)
	}

	secret, err := ddi.k8sClient.CoreV1().Secrets(namespace).Get(ctx, "mdai-datadog-integration", metav1.GetOptions{})
	isNotFound := k8serrors.IsNotFound(err)
	if err != nil && !isNotFound {
		return fmt.Errorf("failed to fetch secret %s: %w", "mdai-datadog-integration", err)
	}

	if isNotFound {
		// Create the secret if it does not exist
		return createIntegrationSecret(ctx, ddi.k8sClient, namespace, "mdai-datadog-integration", integrationName, jsonData)
	} else {
		// Update the secret if it already exists
		return updateSecretWithIntegration(ctx, ddi.k8sClient, namespace, secret, integrationName, jsonData)
	}
}

// DeleteIntegration removes a named integration from the specific integration type's secret.
// Note: This doesn't need a generic type parameter because it only removes a byte array by its map key.
func (ddi *DataDogIntegration) DeleteIntegration(ctx context.Context, namespace, integrationName string) error {
	secret, err := ddi.k8sClient.CoreV1().Secrets(namespace).Get(ctx, "mdai-datadog-integration", metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to fetch secret %s: %w", "mdai-datadog-integration", err)
	}

	if secret.Data == nil {
		return nil
	}
	if _, exists := secret.Data[integrationName]; !exists {
		return nil
	}

	delete(secret.Data, integrationName)

	_, err = ddi.k8sClient.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update secret %s after deletion: %w", "mdai-datadog-integration", err)
	}

	return nil
}

func updateSecretWithIntegration(ctx context.Context, k8sClient kubernetes.Interface, namespace string, secret *corev1.Secret, integrationName string, jsonData []byte) error {
	if secret.Data == nil {
		secret.Data = make(map[string][]byte)
	}
	secret.Data[integrationName] = jsonData

	_, err := k8sClient.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	return err
}

func createIntegrationSecret(ctx context.Context, k8sClient kubernetes.Interface, namespace string, secretName string, integrationName string, jsonData []byte) error {
	newSecret := &corev1.Secret{
		ObjectMeta: metav1.ObjectMeta{
			Name:      secretName,
			Namespace: namespace,
		},
		Data: map[string][]byte{
			integrationName: jsonData,
		},
		Type: corev1.SecretTypeOpaque,
	}

	_, err := k8sClient.CoreV1().Secrets(namespace).Create(ctx, newSecret, metav1.CreateOptions{})
	if err != nil {
		return fmt.Errorf("failed to create secret %s: %w", secretName, err)
	}
	return nil
}
