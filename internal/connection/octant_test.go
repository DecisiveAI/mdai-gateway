package connection

import (
	"context"
	"encoding/json"
	"errors"
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
	k8stesting "k8s.io/client-go/testing"
)

const defaultNamespace = "default"

// --- MOCKS ---

type mockArgoClient struct {
	IntegrationData *integration.ArgoCDIntegrationData
	Err             error
}

func (*mockArgoClient) GetIntegrations(ctx context.Context, namespace string) (map[string]integration.ArgoCDIntegrationData, error) {
	panic("implement me")
}

func (*mockArgoClient) SetIntegration(ctx context.Context, namespace, integrationName string, integrationData integration.ArgoCDIntegrationData) error {
	panic("implement me")
}

func (*mockArgoClient) DeleteIntegration(ctx context.Context, namespace, integrationName string) error {
	panic("implement me")
}

func (m *mockArgoClient) GetIntegrationByName(ctx context.Context, namespace, name string) (*integration.ArgoCDIntegrationData, error) {
	return m.IntegrationData, m.Err
}

type mockDatadogClient struct {
	IntegrationData *integration.DataDogIntegrationData
	Err             error
}

func (*mockDatadogClient) GetIntegrations(ctx context.Context, namespace string) (map[string]integration.DataDogIntegrationData, error) {
	panic("implement me")
}

func (*mockDatadogClient) SetIntegration(ctx context.Context, namespace, integrationName string, integrationData integration.DataDogIntegrationData) error {
	panic("implement me")
}

func (*mockDatadogClient) DeleteIntegration(ctx context.Context, namespace, integrationName string) error {
	panic("implement me")
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

func TestGetConnectionByName_NotFound_NoConfigMap(t *testing.T) {
	t.Parallel()

	// Empty clientset means the connectionsConfigmapName won't exist
	mockK8sClient := fake.NewClientset()
	octantConnection := &OctantConnection{
		k8sClient: mockK8sClient,
	}

	actual, err := octantConnection.GetConnectionByName(context.Background(), defaultNamespace, "team-a")

	require.NoError(t, err)
	assert.Nil(t, actual)
}

func TestGetConnectionByName_NotFound_KeyMissing(t *testing.T) {
	t.Parallel()

	existingObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: connectionsConfigmapName, Namespace: defaultNamespace},
			Data: map[string]string{
				"some-other-team": `{}`,
			},
		},
	}
	mockK8sClient := fake.NewClientset(existingObjects...)
	octantConnection := &OctantConnection{
		k8sClient: mockK8sClient,
	}

	actual, err := octantConnection.GetConnectionByName(context.Background(), defaultNamespace, "team-a")

	require.NoError(t, err)
	assert.Nil(t, actual)
}

func TestGetConnectionByName_Error_ConfigMapGetFailed(t *testing.T) {
	t.Parallel()

	mockK8sClient := fake.NewClientset()
	// Inject a simulated k8s API error
	mockK8sClient.PrependReactor("get", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected get error")
	})

	octantConnection := &OctantConnection{
		k8sClient: mockK8sClient,
	}

	_, err := octantConnection.GetConnectionByName(context.Background(), defaultNamespace, "team-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to get configmap")
	assert.Contains(t, err.Error(), "injected get error")
}

func TestGetConnectionByName_Error_InvalidJSON(t *testing.T) {
	t.Parallel()

	existingObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: connectionsConfigmapName, Namespace: defaultNamespace},
			Data: map[string]string{
				"team-a": "{ invalid json ",
			},
		},
	}
	mockK8sClient := fake.NewClientset(existingObjects...)
	octantConnection := &OctantConnection{
		k8sClient: mockK8sClient,
	}

	_, err := octantConnection.GetConnectionByName(context.Background(), defaultNamespace, "team-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal connection data")
}

func TestGetConnectionByName_Error_ArgoStatusFailed(t *testing.T) {
	t.Parallel()

	// Stand up a server that only returns 500s
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	validConnection := OctantConnectionData{
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
				APIUrl: ts.URL,
			},
		},
	}

	_, err = octantConnection.GetConnectionByName(context.Background(), defaultNamespace, "team-a")
	require.Error(t, err)
	// getArgoAppStatus will fail due to the 500 response
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

func TestSaveConnection_UpdateExistingConfigMap(t *testing.T) {
	t.Parallel()

	ts := setupTestServer()
	defer ts.Close()

	// Pre-populate the ConfigMap to trigger the "update" branch rather than "create"
	existingObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: connectionsConfigmapName, Namespace: defaultNamespace},
			Data: map[string]string{
				"existing-team": `{"sourceType":"datadog"}`,
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

	newConnection := OctantConnectionData{
		Deployment: &Deployment{
			// Use a valid type that bypasses the argo push for a clean test
			Type: ArgoManifestsDeploymentType,
		},
	}

	err := octantConnection.SaveConnection(context.Background(), newConnection, defaultNamespace, "team-a")
	require.NoError(t, err)

	// Verify ConfigMap was updated, not overwritten
	cm, err := mockK8sClient.CoreV1().ConfigMaps(defaultNamespace).Get(context.Background(), connectionsConfigmapName, metav1.GetOptions{})
	require.NoError(t, err)
	require.Contains(t, cm.Data, "team-a")
	require.Contains(t, cm.Data, "existing-team")
}

func TestSaveConnection_Error_InvalidDeploymentType(t *testing.T) {
	t.Parallel()

	// Setup a connection with a deployment type not in validDeploymentTypes
	invalidConnection := OctantConnectionData{
		Deployment: &Deployment{
			Type: "invalid-deployment-type",
		},
	}

	octantConnection := &OctantConnection{}

	err := octantConnection.SaveConnection(context.Background(), invalidConnection, defaultNamespace, "team-a")

	require.Error(t, err)
	assert.Contains(t, err.Error(), "invalid deployment type: invalid-deployment-type")
}

func TestSaveConnection_Error_ConfigMapGetFailed(t *testing.T) {
	t.Parallel()

	mockK8sClient := fake.NewClientset()
	mockK8sClient.PrependReactor("get", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected get error")
	})

	octantConnection := &OctantConnection{
		k8sClient: mockK8sClient,
	}

	// Must provide a valid Deployment Type to bypass the initial validation check
	validConnection := OctantConnectionData{
		Deployment: &Deployment{
			Type: ArgoManifestsDeploymentType,
		},
	}

	err := octantConnection.SaveConnection(context.Background(), validConnection, defaultNamespace, "team-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch configmap")
}

func TestSaveConnection_Error_ArgoPushFailed(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	// Provide an existing configmap so we bypass the create/update configmap logic
	// and go straight to the Argo push. (Assuming updateConfigMapWithConnection doesn't fail here)
	existingObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: connectionsConfigmapName, Namespace: defaultNamespace},
			Data:       map[string]string{},
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

	connection := OctantConnectionData{
		Deployment: &Deployment{
			Type: ArgoSideloadDeploymentType,
		},
	}

	err := octantConnection.SaveConnection(context.Background(), connection, defaultNamespace, "team-a")
	require.Error(t, err)
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

func TestDeleteConnection_NotFound_SilentlyReturns(t *testing.T) {
	t.Parallel()

	// Empty clientset
	mockK8sClient := fake.NewClientset()
	octantConnection := &OctantConnection{
		k8sClient: mockK8sClient,
	}

	// Should safely return nil if the configmap doesn't exist
	err := octantConnection.DeleteConnection(context.Background(), defaultNamespace, "team-a")
	require.NoError(t, err)
}

func TestDeleteConnection_KeyMissing_SilentlyReturns(t *testing.T) {
	t.Parallel()

	existingObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: connectionsConfigmapName, Namespace: defaultNamespace},
			Data: map[string]string{
				"some-other-team": `{}`,
			},
		},
	}
	mockK8sClient := fake.NewClientset(existingObjects...)
	octantConnection := &OctantConnection{
		k8sClient: mockK8sClient,
	}

	// Should safely return nil if the key doesn't exist in the ConfigMap
	err := octantConnection.DeleteConnection(context.Background(), defaultNamespace, "team-a")
	require.NoError(t, err)
}

func TestDeleteConnection_Error_ConfigMapGetFailed(t *testing.T) {
	t.Parallel()

	mockK8sClient := fake.NewClientset()
	mockK8sClient.PrependReactor("get", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected get error")
	})

	octantConnection := &OctantConnection{
		k8sClient: mockK8sClient,
	}

	err := octantConnection.DeleteConnection(context.Background(), defaultNamespace, "team-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to fetch configmap")
}

func TestDeleteConnection_Error_InvalidJSON(t *testing.T) {
	t.Parallel()

	existingObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: connectionsConfigmapName, Namespace: defaultNamespace},
			Data: map[string]string{
				"team-a": "{ invalid json ",
			},
		},
	}
	mockK8sClient := fake.NewClientset(existingObjects...)
	octantConnection := &OctantConnection{
		k8sClient: mockK8sClient,
	}

	err := octantConnection.DeleteConnection(context.Background(), defaultNamespace, "team-a")
	require.Error(t, err)
	assert.Contains(t, err.Error(), "failed to unmarshal connection data")
}

func TestDeleteConnection_Error_ArgoDeleteFailed(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
	}))
	defer ts.Close()

	connection := OctantConnectionData{
		Deployment: &Deployment{
			Type: ArgoSideloadDeploymentType,
		},
	}
	connBytes, err := json.Marshal(connection)
	require.NoError(t, err)

	existingObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: connectionsConfigmapName, Namespace: defaultNamespace},
			Data: map[string]string{
				"team-a": string(connBytes),
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
	require.Error(t, deleteErr)
}

func TestDeleteConnection_Error_ConfigMapUpdateFailed(t *testing.T) {
	t.Parallel()

	connection := OctantConnectionData{}
	connBytes, err := json.Marshal(connection)
	require.NoError(t, err)

	existingObjects := []runtime.Object{
		&corev1.ConfigMap{
			ObjectMeta: metav1.ObjectMeta{Name: connectionsConfigmapName, Namespace: defaultNamespace},
			Data: map[string]string{
				"team-a": string(connBytes),
			},
		},
	}
	mockK8sClient := fake.NewClientset(existingObjects...)

	// Inject failure specifically for the Update call when saving the modified ConfigMap
	mockK8sClient.PrependReactor("update", "configmaps", func(action k8stesting.Action) (bool, runtime.Object, error) {
		return true, nil, errors.New("injected update error")
	})

	octantConnection := &OctantConnection{
		k8sClient: mockK8sClient,
	}

	updateErr := octantConnection.DeleteConnection(context.Background(), defaultNamespace, "team-a")
	require.Error(t, updateErr)
	assert.Contains(t, updateErr.Error(), "failed to update configmap")
}
