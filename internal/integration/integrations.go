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

type IntegrationType string

func getSecretName(integrationType IntegrationType) string {
	return fmt.Sprintf("mdai-%s-integration", integrationType)
}

// GetIntegrationsFromSecret retrieves all integrations of a specific type from its corresponding secret. T represents the integration model (e.g., DataDogIntegration).
func GetIntegrationsFromSecret[T any](ctx context.Context, k8sClient kubernetes.Interface, namespace string, intType IntegrationType) (map[string]T, error) {
	secretName := getSecretName(intType)

	secret, err := k8sClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return map[string]T{}, nil
		}
		return nil, fmt.Errorf("failed to get secret %s: %w", secretName, err)
	}

	integrations := make(map[string]T)
	for name, data := range secret.Data {
		var payload T
		if err := json.Unmarshal(data, &payload); err != nil {
			continue // Skip invalid JSON entries
		}
		integrations[name] = payload
	}

	return integrations, nil
}

// SetIntegration adds or updates a named integration payload in the specified integration type's secret. T represents the integration model (e.g., DataDogIntegration).
func SetIntegration[T any](ctx context.Context, k8sClient kubernetes.Interface, namespace, integrationName string, intType IntegrationType, payload T) error {
	secretName := getSecretName(intType)

	jsonData, err := json.Marshal(payload)
	if err != nil {
		return fmt.Errorf("failed to marshal integration data: %w", err)
	}

	secret, err := k8sClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	isNotFound := k8serrors.IsNotFound(err)
	if err != nil && !isNotFound {
		return fmt.Errorf("failed to fetch secret %s: %w", secretName, err)
	}

	if isNotFound {
		// Create the secret if it does not exist
		return createIntegrationSecret(ctx, k8sClient, namespace, secretName, integrationName, jsonData)
	} else {
		// Update the secret if it already exists
		return updateSecretWithIntegration(ctx, k8sClient, namespace, secret, integrationName, jsonData)
	}
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

// DeleteIntegration removes a named integration from the specific integration type's secret.
// Note: This doesn't need a generic type parameter because it only removes a byte array by its map key.
func DeleteIntegration(ctx context.Context, k8sClient kubernetes.Interface, namespace, integrationName string, intType IntegrationType) error {
	secretName := getSecretName(intType)

	secret, err := k8sClient.CoreV1().Secrets(namespace).Get(ctx, secretName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to fetch secret %s: %w", secretName, err)
	}

	if secret.Data == nil {
		return nil
	}
	if _, exists := secret.Data[integrationName]; !exists {
		return nil
	}

	delete(secret.Data, integrationName)

	_, err = k8sClient.CoreV1().Secrets(namespace).Update(ctx, secret, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update secret %s after deletion: %w", secretName, err)
	}

	return nil
}
