package integration

import (
	"context"
	"encoding/json"
	"reflect"
	"testing"

	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

func TestGetIntegrationsFromSecret(t *testing.T) {
	namespace := "default"
	secretName := getSecretName(DataDogIntegrationType)

	validInt := DataDogIntegration{ApiKey: "12345", DDUrl: "https://example.com"}
	validIntBytes, _ := json.Marshal(validInt)

	tests := []struct {
		name            string
		existingObjects []runtime.Object
		expected        map[string]DataDogIntegration
		wantErr         bool
	}{
		{
			name:            "secret does not exist",
			existingObjects: nil,
			expected:        map[string]DataDogIntegration{},
			wantErr:         false,
		},
		{
			name: "secret exists with valid integrations",
			existingObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
					Data: map[string][]byte{
						"team-a": validIntBytes,
					},
				},
			},
			expected: map[string]DataDogIntegration{
				"team-a": validInt,
			},
			wantErr: false,
		},
		{
			name: "secret exists with invalid json skips the bad entry",
			existingObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
					Data: map[string][]byte{
						"team-a": validIntBytes,
						"team-b": []byte("invalid-json"),
					},
				},
			},
			expected: map[string]DataDogIntegration{
				"team-a": validInt,
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existingObjects...)
			// Explicitly passing [DataDogIntegration] and the IntegrationType
			got, err := GetIntegrationsFromSecret[DataDogIntegration](context.Background(), client, namespace, DataDogIntegrationType)

			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error: %v, got: %v", tt.wantErr, err)
			}

			if !reflect.DeepEqual(got, tt.expected) {
				t.Errorf("expected %v, got %v", tt.expected, got)
			}
		})
	}
}

func TestAddIntegration(t *testing.T) {
	namespace := "default"
	secretName := getSecretName(DataDogIntegrationType)
	newIntegration := DataDogIntegration{ApiKey: "new-key", DDUrl: "https://example.com"}

	tests := []struct {
		name            string
		integrationName string
		existingObjects []runtime.Object
		wantErr         bool
	}{
		{
			name:            "creates secret when it does not exist",
			integrationName: "team-a",
			existingObjects: nil,
			wantErr:         false,
		},
		{
			name:            "updates existing secret",
			integrationName: "team-b",
			existingObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
					Data: map[string][]byte{
						"team-a": []byte(`{"api_key":"old-key","dd_url":"old-url"}`),
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existingObjects...)
			// Go infers [DataDogIntegration] from newIntegration, so we just pass the interface, name, type, and struct
			err := SetIntegration(context.Background(), client, namespace, tt.integrationName, DataDogIntegrationType, newIntegration)

			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error: %v, got: %v", tt.wantErr, err)
			}

			// Verify the secret actually contains the added integration
			secret, _ := client.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
			if secret.Data == nil || len(secret.Data[tt.integrationName]) == 0 {
				t.Errorf("expected secret to contain integration %s, but it was missing", tt.integrationName)
			}
		})
	}
}

func TestDeleteIntegration(t *testing.T) {
	namespace := "default"
	secretName := getSecretName(DataDogIntegrationType)

	tests := []struct {
		name            string
		integrationName string
		existingObjects []runtime.Object
		wantErr         bool
	}{
		{
			name:            "secret does not exist - silently succeeds",
			integrationName: "team-a",
			existingObjects: nil,
			wantErr:         false,
		},
		{
			name:            "integration does not exist in secret - silently succeeds",
			integrationName: "team-b",
			existingObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
					Data: map[string][]byte{
						"team-a": []byte(`{"api_key":"key","dd_url":"url"}`),
					},
				},
			},
			wantErr: false,
		},
		{
			name:            "successfully deletes existing integration",
			integrationName: "team-a",
			existingObjects: []runtime.Object{
				&corev1.Secret{
					ObjectMeta: metav1.ObjectMeta{Name: secretName, Namespace: namespace},
					Data: map[string][]byte{
						"team-a": []byte(`{"api_key":"key","dd_url":"url"}`),
						"team-b": []byte(`{"api_key":"key2","dd_url":"url2"}`),
					},
				},
			},
			wantErr: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			client := fake.NewSimpleClientset(tt.existingObjects...)
			// Calling the new generic-friendly delete function
			err := DeleteIntegration(context.Background(), client, namespace, tt.integrationName, DataDogIntegrationType)

			if (err != nil) != tt.wantErr {
				t.Fatalf("expected error: %v, got: %v", tt.wantErr, err)
			}

			// Ensure the target integration is gone
			if len(tt.existingObjects) > 0 {
				secret, _ := client.CoreV1().Secrets(namespace).Get(context.Background(), secretName, metav1.GetOptions{})
				if _, exists := secret.Data[tt.integrationName]; exists {
					t.Errorf("expected integration %s to be deleted, but it was still found", tt.integrationName)
				}
			}
		})
	}
}
