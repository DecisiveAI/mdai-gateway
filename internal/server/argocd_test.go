package server

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	integrationmock "github.com/mydecisive/mdai-gateway/internal/mock/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
)

func TestArgoCD_GetIntegrations(t *testing.T) {
	t.Parallel()

	t.Run("error retrieving integrations", func(t *testing.T) {
		t.Parallel()

		integrationMock := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
		integrationMock.EXPECT().GetIntegrations(mock.Anything, "default").Return(nil, assert.AnError).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/getIntegrations", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupArgoCDRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		expectedIntegrations := map[string]integration.ArgoCDIntegrationData{
			"integration1": {
				AccountToken: "abc123",
			},
			"integration2": {
				AccountToken: "xyz999",
			},
		}

		integrationMock := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
		integrationMock.EXPECT().GetIntegrations(mock.Anything, "default").Return(expectedIntegrations, nil).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/getIntegrations", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupArgoCDRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var integrationList []string
		err := json.Unmarshal(resp.Body.Bytes(), &integrationList)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"integration1", "integration2"}, integrationList)
	})
}

func TestArgoCD_PutIntegrationData(t *testing.T) {
	t.Parallel()

	t.Run("invalid request payload", func(t *testing.T) {
		t.Parallel()

		integrationMock := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)

		invalidPayoad, err := json.Marshal("not valid json")
		require.NoError(t, err)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/putIntegration/coolIntegration", bytes.NewBuffer(invalidPayoad))
		resp := httptest.NewRecorder()

		router := setupArgoCDRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("error setting the integration", func(t *testing.T) {
		t.Parallel()

		integrationToSave := integration.ArgoCDIntegrationData{
			AccountToken: "abc123",
		}
		serializedIntegration, err := json.Marshal(integrationToSave)
		require.NoError(t, err)

		integrationMock := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
		integrationMock.EXPECT().
			SetIntegration(mock.Anything, "default", "coolIntegration", mock.MatchedBy(func(integrationData any) bool {
				argocdIntegrationData, ok := integrationData.(integration.ArgoCDIntegrationData)
				return ok && argocdIntegrationData.AccountToken == "abc123"
			})).
			Return(assert.AnError).
			Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/putIntegration/coolIntegration", bytes.NewBuffer(serializedIntegration))
		resp := httptest.NewRecorder()

		router := setupArgoCDRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		integrationToSave := integration.ArgoCDIntegrationData{
			AccountToken: "abc123",
		}
		serializedIntegration, err := json.Marshal(integrationToSave)
		require.NoError(t, err)

		integrationMock := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
		integrationMock.EXPECT().
			SetIntegration(mock.Anything, "default", "coolIntegration", mock.MatchedBy(func(integrationData any) bool {
				argocdIntegrationData, ok := integrationData.(integration.ArgoCDIntegrationData)
				return ok && argocdIntegrationData.AccountToken == "abc123"
			})).
			Return(nil).
			Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/putIntegration/coolIntegration", bytes.NewBuffer(serializedIntegration))
		resp := httptest.NewRecorder()

		router := setupArgoCDRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
	})
}

func TestArgoCD_DeleteIntegration(t *testing.T) {
	t.Parallel()

	t.Run("error deleting integration", func(t *testing.T) {
		t.Parallel()

		integrationMock := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
		integrationMock.EXPECT().DeleteIntegration(mock.Anything, "default", "coolIntegration").Return(assert.AnError).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/deleteIntegration/coolIntegration", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupArgoCDRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		integrationMock := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
		integrationMock.EXPECT().DeleteIntegration(mock.Anything, "default", "coolIntegration").Return(nil).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/deleteIntegration/coolIntegration", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupArgoCDRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
	})
}

func setupArgoCDRouter(t *testing.T, theIntegration integration.Integration[integration.ArgoCDIntegrationData]) *http.ServeMux {
	t.Helper()

	argoHandler := NewArgoCDHandler(theIntegration, "default", zaptest.NewLogger(t))

	mainRouter := http.NewServeMux()
	mainRouter.Handle("/getIntegrations", argoHandler.GetIntegrations(t.Context()))
	mainRouter.Handle("/putIntegration/{integrationName}", argoHandler.PutIntegrationData(t.Context()))
	mainRouter.Handle("/deleteIntegration/{integrationName}", argoHandler.DeleteIntegration(t.Context()))
	return mainRouter
}
