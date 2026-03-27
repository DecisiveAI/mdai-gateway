package connection

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes/fake"
)

const defaultNamespace = "default"

// --- MOCKS ---

type mockArgoClient struct {
	IntegrationData *integration.ArgoCDIntegrationData
	Err             error
}

func (m *mockArgoClient) GetIntegrationByName(ctx context.Context, namespace, name string) (*integration.ArgoCDIntegrationData, error) {
	return m.IntegrationData, m.Err
}

type mockDatadogClient struct {
	IntegrationData *integration.DataDogIntegrationData
	Err             error
}

func (m *mockDatadogClient) GetIntegrationByName(ctx context.Context, namespace, name string) (*integration.DataDogIntegrationData, error) {
	return m.IntegrationData, m.Err
}

// Helper to stand up a fake Argo API.
func setupTestServer() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")

		// Return success for all standard operations
		switch {
		case r.Method == http.MethodGet && r.URL.Path == "/api/v1/applications/team-a":
			w.WriteHeader(http.StatusOK)
			w.Write([]byte(`{"status": {"health": {"status": "Healthy"}}}`)) // nolint: errcheck,gosec,revive
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/team-a/sync":
			w.WriteHeader(http.StatusOK)
		case r.Method == http.MethodDelete && r.URL.Path == "/api/v1/applications/team-a":
			w.WriteHeader(http.StatusOK)
		default:
			w.WriteHeader(http.StatusOK) // Catch-all for tests not strictly checking response bodies
		}
	}))
}

// --- TESTS ---

func TestGetConnectionByName(t *testing.T) {
	t.Parallel()

	ts := setupTestServer()
	defer ts.Close()

	validConnection := OctantConnectionData{
		SourceType: "datadog",
		Deployment: &Deployment{
			Type:            ArgoSideloadDeploymentType,
			IntegrationName: "argo-test",
		},
	}
	validConnectionBytes, err := json.Marshal(validConnection)
	require.NoError(t, err)

	existingObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: connectionsConfigmapName, Namespace: defaultNamespace},
			Data: map[string]string{
				"team-a": string(validConnectionBytes),
			},
		},
	}

	mockK8sClient := fake.NewClientset(existingObjects...)
	octantConnection := &OctantConnection{
		httpClient: ts.Client(),
		k8sClient:  mockK8sClient,
		argoClient: &mockArgoClient{
			IntegrationData: &integration.ArgoCDIntegrationData{
				APIUrl:       ts.URL,
				AccountToken: "fake-token",
			},
		},
	}

	actual, getErr := octantConnection.GetConnectionByName(context.Background(), defaultNamespace, "team-a")
	require.NoError(t, getErr)
	require.NotNil(t, actual)

	// Validate that the status was successfully fetched from our mock server
	statusMap, ok := actual.Status.(*ArgoApp)
	require.True(t, ok)
	assert.Equal(t, "Healthy", statusMap.Status.Health.Status)
}

func TestSaveConnection(t *testing.T) {
	t.Parallel()

	ts := setupTestServer()
	defer ts.Close()

	newConnection := OctantConnectionData{
		SourceType: "datadog",
		Destinations: []OctantConnectionDestination{
			{DestinationType: "datadog", IntegrationName: "dd-test"},
		},
		Deployment: &Deployment{
			Type:            ArgoSideloadDeploymentType,
			IntegrationName: "argo-test",
		},
	}

	mockK8sClient := fake.NewClientset()
	octantConnection := &OctantConnection{
		httpClient: ts.Client(),
		k8sClient:  mockK8sClient,
		argoClient: &mockArgoClient{
			IntegrationData: &integration.ArgoCDIntegrationData{
				APIUrl: ts.URL,
			},
		},
		datadogClient: &mockDatadogClient{
			IntegrationData: &integration.DataDogIntegrationData{},
		},
	}

	err := octantConnection.SaveConnection(context.Background(), newConnection, defaultNamespace, "team-a")
	require.NoError(t, err)

	// Verify ConfigMap was created
	cm, err := mockK8sClient.CoreV1().ConfigMaps(defaultNamespace).Get(context.Background(), connectionsConfigmapName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, cm.Data, "team-a")
}

func TestDeleteConnection(t *testing.T) {
	t.Parallel()

	ts := setupTestServer()
	defer ts.Close()

	existingConnection := OctantConnectionData{
		SourceType: "datadog",
		Deployment: &Deployment{
			Type:            ArgoSideloadDeploymentType,
			IntegrationName: "argo-test",
		},
	}
	existingConnectionBytes, marshalErr := json.Marshal(existingConnection)
	require.NoError(t, marshalErr)

	existingObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: connectionsConfigmapName, Namespace: defaultNamespace},
			Data: map[string]string{
				"team-a": string(existingConnectionBytes),
			},
		},
	}

	mockK8sClient := fake.NewClientset(existingObjects...)
	octantConnection := &OctantConnection{
		httpClient: ts.Client(),
		k8sClient:  mockK8sClient,
		argoClient: &mockArgoClient{
			IntegrationData: &integration.ArgoCDIntegrationData{
				APIUrl: ts.URL,
			},
		},
	}

	deleteErr := octantConnection.DeleteConnection(context.Background(), defaultNamespace, "team-a")
	require.NoError(t, deleteErr)

	// Verify removed
	cm, getCMErr := mockK8sClient.CoreV1().ConfigMaps(defaultNamespace).Get(context.Background(), connectionsConfigmapName, metav1.GetOptions{})
	require.NoError(t, getCMErr)
	require.NotContains(t, cm.Data, "team-a")
}
