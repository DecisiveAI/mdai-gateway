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
			TelemetryTypes: []connection.Telemetry{
				connection.Logs,
				connection.Traces,
			},
			Deployment: &connection.Deployment{
				Type: "argocd",
				Fields: map[string]any{
					"branch": "bestBranch",
				},
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

func TestSaveConnectionData(t *testing.T) {
	t.Parallel()

	t.Run("invalid request payload", func(t *testing.T) {
		t.Parallel()

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)

		invalidPayoad, err := json.Marshal("not valid json")
		require.NoError(t, err)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/saveConnection/coolConnection", bytes.NewBuffer(invalidPayoad))
		resp := httptest.NewRecorder()

		router := setupConnectionsRouter(t, connectionsMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("error saving the connection", func(t *testing.T) {
		t.Parallel()

		connectionToSave := &connection.OctantConnectionData{
			SourceType: "datadog",
			TelemetryTypes: []connection.Telemetry{
				connection.Logs,
				connection.Traces,
			},
			Deployment: &connection.Deployment{
				Type: "argocd",
				Fields: map[string]any{
					"branch": "bestBranch",
				},
			},
		}
		serializedConnection, err := json.Marshal(connectionToSave)
		require.NoError(t, err)

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		connectionsMock.EXPECT().
			SaveConnection(mock.Anything, mock.MatchedBy(func(theConnection connection.OctantConnectionData) bool {
				matchingSource := theConnection.SourceType == "datadog"
				matchingTelemetry := len(theConnection.TelemetryTypes) == 2 && theConnection.TelemetryTypes[0] == connection.Logs && theConnection.TelemetryTypes[1] == connection.Traces
				matchingDeployment := theConnection.Deployment.Type == "argocd" && theConnection.Deployment.Fields["branch"] == "bestBranch"
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
			TelemetryTypes: []connection.Telemetry{
				connection.Logs,
				connection.Traces,
			},
			Deployment: &connection.Deployment{
				Type: "argocd",
				Fields: map[string]any{
					"branch": "bestBranch",
				},
			},
		}
		serializedConnection, err := json.Marshal(connectionToSave)
		require.NoError(t, err)

		connectionsMock := connectionmock.NewMockConnection[connection.OctantConnectionData](t)
		connectionsMock.EXPECT().
			SaveConnection(mock.Anything, mock.MatchedBy(func(theConnection connection.OctantConnectionData) bool {
				matchingSource := theConnection.SourceType == "datadog"
				matchingTelemetry := len(theConnection.TelemetryTypes) == 2 && theConnection.TelemetryTypes[0] == connection.Logs && theConnection.TelemetryTypes[1] == connection.Traces
				matchingDeployment := theConnection.Deployment.Type == "argocd" && theConnection.Deployment.Fields["branch"] == "bestBranch"
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

func setupConnectionsRouter(t *testing.T, theConnection connection.Connection[connection.OctantConnectionData]) *http.ServeMux {
	t.Helper()

	connectionsHandler := NewConnectionsHandler(theConnection, "default", zaptest.NewLogger(t))

	mainRouter := http.NewServeMux()
	mainRouter.Handle("/getConnection/{connectionName}", connectionsHandler.GetConnectionByName(t.Context()))
	mainRouter.Handle("/saveConnection/{connectionName}", connectionsHandler.SaveConnectionData(t.Context()))
	mainRouter.Handle("/deleteConnection/{connectionName}", connectionsHandler.DeleteConnectionByName(t.Context()))
	return mainRouter
}
