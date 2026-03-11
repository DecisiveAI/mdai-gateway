package server

import (
	"encoding/json"
	"github.com/mydecisive/mdai-gateway/internal/integration"
	integrationmock "github.com/mydecisive/mdai-gateway/internal/mock/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap/zaptest"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetIntegrations(t *testing.T) {
	t.Parallel()

	t.Run("happy path", func(t *testing.T) {
		t.Parallel()

		expectedIntegrations := map[string]any{
			"integration1": integration.DataDogIntegrationData{
				ApiKey: "abc123",
				DDUrl:  "http://datadog.example.com",
			},
			"integration2": integration.DataDogIntegrationData{
				ApiKey: "xyz999",
				DDUrl:  "http://datadog.example.com",
			},
		}

		integrationMock := integrationmock.NewMockIntegration(t)
		integrationMock.EXPECT().GetIntegrations(mock.Anything, "default").Return(expectedIntegrations, nil).Times(1)

		req := httptest.NewRequest(http.MethodGet, "/test", http.NoBody)
		resp := httptest.NewRecorder()

		router := setupRouter(t, integrationMock)
		router.ServeHTTP(resp, req)

		assert.Equal(t, http.StatusOK, resp.Code)

		var integrationList []string
		err := json.Unmarshal(resp.Body.Bytes(), &integrationList)
		require.NoError(t, err)
		assert.ElementsMatch(t, []string{"integration1", "integration2"}, integrationList)
	})
}

func setupRouter(t *testing.T, integration integration.Integration) *http.ServeMux {
	ddh := NewDatadogHandler(integration, "default", zaptest.NewLogger(t))

	mainRouter := http.NewServeMux()
	mainRouter.Handle("/test", ddh.GetIntegrations(t.Context()))
	return mainRouter
}
