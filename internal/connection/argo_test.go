package connection

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestGetArgoAppStatus(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name           string
		serverResponse int
		responseBody   string
		expectError    bool
	}{
		{
			name:           "success",
			serverResponse: http.StatusOK,
			responseBody:   `{"status": {"health": {"status": "Healthy"}}}`,
			expectError:    false,
		},
		{
			name:           "argo error",
			serverResponse: http.StatusNotFound,
			responseBody:   `{"error": "not found"}`,
			expectError:    true,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				assert.Equal(t, "/api/v1/applications/my-app", r.URL.Path)
				assert.Equal(t, "upsert=true", r.URL.RawQuery)
				assert.Equal(t, "Bearer fake-token", r.Header.Get("Authorization"))

				w.WriteHeader(tc.serverResponse)
				w.Write([]byte(tc.responseBody))
			}))
			defer ts.Close()

			oc := &OctantConnection{
				httpClient: ts.Client(),
				argoClient: &mockArgoClient{
					IntegrationData: &integration.ArgoCDIntegrationData{
						APIUrl:       ts.URL,
						AccountToken: "fake-token",
					},
				},
			}

			app, err := oc.getArgoAppStatus(context.Background(), "my-app", "default", OctantConnectionData{
				Deployment: &Deployment{IntegrationName: "argo-test"},
			})

			if tc.expectError {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Equal(t, "Healthy", app.Status.Health.Status)
			}
		})
	}
}

func TestDeleteArgoApp(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		assert.Equal(t, "DELETE", r.Method)
		assert.Equal(t, "/api/v1/applications/my-app", r.URL.Path)
		assert.Contains(t, r.URL.RawQuery, "cascade=true")
		w.WriteHeader(http.StatusOK)
	}))
	defer ts.Close()

	oc := &OctantConnection{
		httpClient: ts.Client(),
		argoClient: &mockArgoClient{
			IntegrationData: &integration.ArgoCDIntegrationData{APIUrl: ts.URL},
		},
	}

	err := oc.deleteArgoApp(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})
	require.NoError(t, err)
}

func TestPushArgoApp(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name               string
		destinations       []OctantConnectionDestination
		ddClientErr        error
		argoClientErr      error
		createResponseCode int
		syncResponseCode   int
		expectedErr        string
	}{
		{
			name: "multiple destinations unsupported",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
				{DestinationType: "datadog", IntegrationName: "dd-2"},
			},
			expectedErr: "pushing argo application to multiple destinations is currently unsupported",
		},
		{
			name: "unknown destination type",
			destinations: []OctantConnectionDestination{
				{DestinationType: "newrelic", IntegrationName: "nr-1"},
			},
			expectedErr: "unknown destination type: newrelic",
		},
		{
			name: "datadog integration fetch fails",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
			},
			ddClientErr: errors.New("datadog integration not found"),
			expectedErr: "datadog integration not found",
		},
		{
			name: "argo integration fetch fails",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
			},
			argoClientErr: errors.New("argo integration not found"),
			expectedErr:   "argo integration not found",
		},
		{
			name: "app creation HTTP call fails",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
			},
			createResponseCode: http.StatusInternalServerError,
			expectedErr:        "unexpected status code: 500",
		},
		{
			name: "app sync HTTP call fails",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
			},
			createResponseCode: http.StatusOK,
			syncResponseCode:   http.StatusBadRequest,
			expectedErr:        "unexpected status code: 400",
		},
		{
			name: "success path",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
			},
			createResponseCode: http.StatusOK,
			syncResponseCode:   http.StatusOK,
			expectedErr:        "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			// Stand up an httptest server specifically for this test case
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")

				// Route: Create App
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications" {
					w.WriteHeader(tc.createResponseCode)
					return
				}

				// Route: Sync App
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/my-test-app/sync" {
					w.WriteHeader(tc.syncResponseCode)
					return
				}

				// Catch-all
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			oc := &OctantConnection{
				httpClient: ts.Client(),
				argoClient: &mockArgoClient{
					IntegrationData: &integration.ArgoCDIntegrationData{
						APIUrl:       ts.URL,
						AccountToken: "fake-token",
					},
					Err: tc.argoClientErr,
				},
				datadogClient: &mockDatadogClient{
					IntegrationData: &integration.DataDogIntegrationData{},
					Err:             tc.ddClientErr,
				},
			}

			connData := OctantConnectionData{
				Destinations: tc.destinations,
				Deployment: &Deployment{
					IntegrationName: "argo-test",
				},
			}

			// Execute
			err := oc.pushArgoApp(context.Background(), "default", "my-test-app", connData)

			// Assert
			if tc.expectedErr != "" {
				require.ErrorContains(t, err, tc.expectedErr)
			} else {
				require.NoError(t, err)
			}
		})
	}
}
