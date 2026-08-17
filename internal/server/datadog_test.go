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

func TestGetIntegrations(t *testing.T) {
	t.Parallel()

	t.Run("error retrieving integrations", func(t *testing.T) {
		t.Parallel()

		integrationMock := integrationmock.NewMockIntegration[integration.DataDogIntegrationData](t)
		integrationMock.EXPECT().GetIntegrations(mock.Anything, "default").Return(nil, assert.AnError).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/getIntegrations", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupDatadogRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		expectedIntegrations := map[string]integration.DataDogIntegrationData{
			"integration1": {
				APIKey: "abc123",
				DDUrl:  "http://datadog.example.com",
			},
			"integration2": {
				APIKey: "xyz999",
				DDUrl:  "http://datadog.example.com",
			},
		}

		integrationMock := integrationmock.NewMockIntegration[integration.DataDogIntegrationData](t)
		integrationMock.EXPECT().GetIntegrations(mock.Anything, "default").Return(expectedIntegrations, nil).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/getIntegrations", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupDatadogRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var integrationList []string
		err := json.Unmarshal(resp.Body.Bytes(), &integrationList)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"integration1", "integration2"}, integrationList)
	})
}

func TestPutIntegrationData(t *testing.T) {
	t.Parallel()

	t.Run("invalid request payload", func(t *testing.T) {
		t.Parallel()

		integrationMock := integrationmock.NewMockIntegration[integration.DataDogIntegrationData](t)

		invalidPayoad, err := json.Marshal("not valid json")
		require.NoError(t, err)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/putIntegration/coolIntegration", bytes.NewBuffer(invalidPayoad))
		resp := httptest.NewRecorder()

		router := setupDatadogRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusBadRequest, resp.Code)
	})

	t.Run("error setting the integration", func(t *testing.T) {
		t.Parallel()

		integrationToSave := integration.DataDogIntegrationData{
			APIKey: "abc123",
			DDUrl:  "http://datadog.example.com",
		}
		serializedIntegration, err := json.Marshal(integrationToSave) //nolint:gosec // test fixture only; not a real Datadog credential.
		require.NoError(t, err)

		integrationMock := integrationmock.NewMockIntegration[integration.DataDogIntegrationData](t)
		integrationMock.EXPECT().
			SetIntegration(mock.Anything, "default", "coolIntegration", mock.MatchedBy(func(integrationData any) bool {
				ddIntegrationData, ok := integrationData.(integration.DataDogIntegrationData)
				return ok && ddIntegrationData.APIKey == "abc123" && ddIntegrationData.DDUrl == "http://datadog.example.com"
			})).
			Return(assert.AnError).
			Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/putIntegration/coolIntegration", bytes.NewBuffer(serializedIntegration))
		resp := httptest.NewRecorder()

		router := setupDatadogRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		integrationToSave := integration.DataDogIntegrationData{
			APIKey: "abc123",
			DDUrl:  "http://datadog.example.com",
		}
		serializedIntegration, err := json.Marshal(integrationToSave) //nolint:gosec // test fixture only; not a real Datadog credential.
		require.NoError(t, err)

		integrationMock := integrationmock.NewMockIntegration[integration.DataDogIntegrationData](t)
		integrationMock.EXPECT().
			SetIntegration(mock.Anything, "default", "coolIntegration", mock.MatchedBy(func(integrationData any) bool {
				ddIntegrationData, ok := integrationData.(integration.DataDogIntegrationData)
				return ok && ddIntegrationData.APIKey == "abc123" && ddIntegrationData.DDUrl == "http://datadog.example.com"
			})).
			Return(nil).
			Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodPut, "/putIntegration/coolIntegration", bytes.NewBuffer(serializedIntegration))
		resp := httptest.NewRecorder()

		router := setupDatadogRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
	})
}

func TestDeleteIntegration(t *testing.T) {
	t.Parallel()

	t.Run("error deleting integration", func(t *testing.T) {
		t.Parallel()

		integrationMock := integrationmock.NewMockIntegration[integration.DataDogIntegrationData](t)
		integrationMock.EXPECT().DeleteIntegration(mock.Anything, "default", "coolIntegration").Return(assert.AnError).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodDelete, "/deleteIntegration/coolIntegration", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupDatadogRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusInternalServerError, resp.Code)
	})

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		integrationMock := integrationmock.NewMockIntegration[integration.DataDogIntegrationData](t)
		integrationMock.EXPECT().DeleteIntegration(mock.Anything, "default", "coolIntegration").Return(nil).Times(1)

		req := httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/deleteIntegration/coolIntegration", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupDatadogRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)
	})
}

func setupDatadogRouter(t *testing.T, theIntegration integration.Integration[integration.DataDogIntegrationData]) *http.ServeMux {
	t.Helper()

	ddh := NewDatadogHandler(theIntegration, "default", zaptest.NewLogger(t))

	mainRouter := http.NewServeMux()
	mainRouter.Handle("/getIntegrations", ddh.GetIntegrations(t.Context()))
	mainRouter.Handle("/putIntegration/{integrationName}", ddh.PutIntegrationData(t.Context()))
	mainRouter.Handle("/deleteIntegration/{integrationName}", ddh.DeleteIntegration(t.Context()))
	return mainRouter
}
