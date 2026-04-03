package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"testing"

	"github.com/mydecisive/mdai-gateway/internal/connection"
	connectionmock "github.com/mydecisive/mdai-gateway/internal/mock/connection"
	"github.com/mydecisive/mdai-gateway/internal/telemetry"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestGetConnectionByName(t *testing.T) {
	t.Parallel()

	t.Run("error retrieving connection", func(t *testing.T) {
		t.Parallel()

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		connectionsMock.EXPECT().GetConnectionByName(mock.Anything, "default", "specialConnection").Return(nil, assert.AnError).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/getConnection/specialConnection", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		expectedConnection := &connection.OctantConnectionData{
			SourceType: "datadog",
			TelemetryTypes: []telemetry.MLT{
				telemetry.Logs,
				telemetry.Traces,
			},
			Deployment: &connection.Deployment{
				Type:            connection.ArgoSideloadDeploymentType,
				IntegrationName: "argo-test",
			},
		}

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		connectionsMock.EXPECT().GetConnectionByName(mock.Anything, "default", "specialConnection").Return(expectedConnection, nil).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/getConnection/specialConnection", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var actualConnection connection.OctantConnectionData
		err := json.Unmarshal(resp.Body.Bytes(), &actualConnection)
		require.NoError(t, err)
		assert.True(t, reflect.DeepEqual(expectedConnection, &actualConnection))
	})
}

func TestGenerateManifestsForGivenConnection(t *testing.T) {
	t.Parallel()

	t.Run("invalid format parameter", func(t *testing.T) {
		t.Parallel()

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		validPayload, err := json.Marshal(connection.OctantConnectionData{})
		require.NoError(t, err)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/generateManifests/coolConnection/xml", bytes.NewBuffer(validPayload))
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "invalid format xml")
	})

	t.Run("invalid request payload", func(t *testing.T) {
		t.Parallel()

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/generateManifests/coolConnection/yaml", bytes.NewBufferString("invalid json"))
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
		assert.Contains(t, resp.Body.String(), "request payload was invalid")
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)

		goodConnection := connection.OctantConnectionData{
			Destinations: []connection.OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "test-dd"},
			},
			Deployment: &connection.Deployment{
				Type: connection.ArgoManifestsDeploymentType,
			},
		}
		payload, err := json.Marshal(goodConnection)
		require.NoError(t, err)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPost, "/generateManifests/coolConnection/yaml", bytes.NewBuffer(payload))
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
		assert.Equal(t, "application/zip", resp.Header().Get("Content-Type"))
		assert.Contains(t, resp.Header().Get("Content-Disposition"), `attachment; filename="coolConnection-manifests-`)
		assert.Positive(t, resp.Body.Len())
	})
}

func TestSaveConnectionData(t *testing.T) {
	t.Parallel()

	t.Run("invalid request payload", func(t *testing.T) {
		t.Parallel()

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)

		invalidPayoad, err := json.Marshal("not valid json")
		require.NoError(t, err)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/saveConnection/coolConnection", bytes.NewBuffer(invalidPayoad))
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("error saving the connection", func(t *testing.T) {
		t.Parallel()

		connectionToSave := &connection.OctantConnectionData{
			SourceType: "datadog",
			TelemetryTypes: []telemetry.MLT{
				telemetry.Logs,
				telemetry.Traces,
			},
			Deployment: &connection.Deployment{
				Type:            connection.ArgoSideloadDeploymentType,
				IntegrationName: "argo-test",
			},
		}
		serializedConnection, err := json.Marshal(connectionToSave)
		require.NoError(t, err)

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		connectionsMock.EXPECT().
			SaveConnection(mock.Anything, mock.MatchedBy(func(theConnection connection.OctantConnectionData) bool {
				matchingSource := theConnection.SourceType == "datadog"
				matchingTelemetry := len(theConnection.TelemetryTypes) == 2 && theConnection.TelemetryTypes[0] == connection.Logs && theConnection.TelemetryTypes[1] == connection.Traces
				matchingDeployment := theConnection.Deployment.Type == connection.ArgoSideloadDeploymentType && theConnection.Deployment.IntegrationName == "argo-test"
				return matchingSource && matchingTelemetry && matchingDeployment
			}), "default", "coolConnection").
			Return(assert.AnError).
			Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/saveConnection/coolConnection", bytes.NewBuffer(serializedConnection))
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		connectionToSave := &connection.OctantConnectionData{
			SourceType: "datadog",
			TelemetryTypes: []telemetry.MLT{
				telemetry.Logs,
				telemetry.Traces,
			},
			Deployment: &connection.Deployment{
				Type:            connection.ArgoSideloadDeploymentType,
				IntegrationName: "argo-test",
			},
		}
		serializedConnection, err := json.Marshal(connectionToSave)
		require.NoError(t, err)

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		connectionsMock.EXPECT().
			SaveConnection(mock.Anything, mock.MatchedBy(func(theConnection connection.OctantConnectionData) bool {
				matchingSource := theConnection.SourceType == "datadog"
				matchingTelemetry := len(theConnection.TelemetryTypes) == 2 && theConnection.TelemetryTypes[0] == connection.Logs && theConnection.TelemetryTypes[1] == connection.Traces
				matchingDeployment := theConnection.Deployment.Type == connection.ArgoSideloadDeploymentType && theConnection.Deployment.IntegrationName == "argo-test"
				return matchingSource && matchingTelemetry && matchingDeployment
			}), "default", "coolConnection").
			Return(nil).
			Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/saveConnection/coolConnection", bytes.NewBuffer(serializedConnection))
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
	})
}

func TestDeleteConnectionByName(t *testing.T) {
	t.Parallel()

	t.Run("error deleting integration", func(t *testing.T) {
		t.Parallel()

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		connectionsMock.EXPECT().DeleteConnection(mock.Anything, "default", "coolConnection").Return(assert.AnError).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/deleteConnection/coolConnection", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		connectionsMock.EXPECT().DeleteConnection(mock.Anything, "default", "coolConnection").Return(nil).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/deleteConnection/coolConnection", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
	})
}

func TestGetConnectionStatus(t *testing.T) {
	t.Parallel()

	t.Run("error getting connection status", func(t *testing.T) {
		t.Parallel()

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		connectionsMock.EXPECT().GetConnectionStatus(mock.Anything, "default", "coolConnection").Return(nil, assert.AnError).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/getConnection/coolConnection/status", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		connectionStatus := &connection.Status{
			ReceivingData: true,
			SendingData:   true,
			DataIntegrity: false,
			Details:       "",
		}

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		connectionsMock.EXPECT().GetConnectionStatus(mock.Anything, "default", "coolConnection").Return(connectionStatus, nil).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/getConnection/coolConnection/status", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var actualStatus connection.Status
		err := json.Unmarshal(resp.Body.Bytes(), &actualStatus)
		require.NoError(t, err)
		assert.True(t, reflect.DeepEqual(connectionStatus, &actualStatus))
	})
}

func setupConnectionsRouter(t *testing.T, theConnection connection.Connection[connection.OctantConnectionData]) *http.ServeMux {
	t.Helper()

	connectionsHandler := NewConnectionsHandler(theConnection, "default", zaptest.NewLogger(t))

	mainRouter := http.NewServeMux()
	mainRouter.Handle("GET /getConnection/{connectionName}", connectionsHandler.GetConnectionByName(t.Context()))
	mainRouter.Handle("POST /generateManifests/{connectionName}/{format}", connectionsHandler.GenerateManifestsForGivenConnection())
	mainRouter.Handle("PUT /saveConnection/{connectionName}", connectionsHandler.SaveConnectionData(t.Context()))
	mainRouter.Handle("DELETE /deleteConnection/{connectionName}", connectionsHandler.DeleteConnectionByName(t.Context()))
	mainRouter.Handle("GET /getConnection/{connectionName}", connectionsHandler.GetConnectionByName(t.Context()))
	return mainRouter
}
