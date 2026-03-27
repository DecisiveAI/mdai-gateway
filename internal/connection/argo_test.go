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
				w.Write([]byte(tc.responseBody)) // nolint: errcheck,gosec
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

func TestGetArgoAppStatus_Error_IntegrationFetchFailed(t *testing.T) {
	t.Parallel()

	oc := &OctantConnection{
		argoClient: &mockArgoClient{
			Err: errors.New("injected argo integration error"),
		},
	}

	_, err := oc.getArgoAppStatus(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "injected argo integration error")
}

func TestGetArgoAppStatus_Error_RequestCreation(t *testing.T) {
	t.Parallel()

	oc := &OctantConnection{
		argoClient: &mockArgoClient{
			IntegrationData: &integration.ArgoCDIntegrationData{
				APIUrl: "://invalid-url", // Forces http.NewRequestWithContext to fail
			},
		},
	}

	_, err := oc.getArgoAppStatus(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
}

func TestGetArgoAppStatus_Error_HTTPDoFailed(t *testing.T) {
	t.Parallel()

	// Create a server and immediately close it so httpClient.Do() fails on connection refused
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()

	oc := &OctantConnection{
		httpClient: ts.Client(),
		argoClient: &mockArgoClient{
			IntegrationData: &integration.ArgoCDIntegrationData{
				APIUrl: ts.URL,
			},
		},
	}

	_, err := oc.getArgoAppStatus(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
}

func TestGetArgoAppStatus_Error_InvalidJSON(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
		w.Write([]byte(`{ "invalid": json `)) // Malformed JSON to fail decode
	}))
	defer ts.Close()

	oc := &OctantConnection{
		httpClient: ts.Client(),
		argoClient: &mockArgoClient{
			IntegrationData: &integration.ArgoCDIntegrationData{APIUrl: ts.URL},
		},
	}

	_, err := oc.getArgoAppStatus(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
}

func TestDeleteArgoApp_Error_IntegrationFetchFailed(t *testing.T) {
	t.Parallel()

	oc := &OctantConnection{
		argoClient: &mockArgoClient{
			Err: errors.New("injected argo integration error"),
		},
	}

	err := oc.deleteArgoApp(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "injected argo integration error")
}

func TestDeleteArgoApp_Error_RequestCreation(t *testing.T) {
	t.Parallel()

	oc := &OctantConnection{
		argoClient: &mockArgoClient{
			IntegrationData: &integration.ArgoCDIntegrationData{
				APIUrl: "://invalid-url", // Forces http.NewRequestWithContext to fail
			},
		},
	}

	err := oc.deleteArgoApp(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
}

func TestDeleteArgoApp_Error_HTTPDoFailed(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close() // Close immediately to force http.Do error

	oc := &OctantConnection{
		httpClient: ts.Client(),
		argoClient: &mockArgoClient{
			IntegrationData: &integration.ArgoCDIntegrationData{APIUrl: ts.URL},
		},
	}

	err := oc.deleteArgoApp(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
}

func TestDeleteArgoApp_Error_BadStatusCode(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusInternalServerError)
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

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status code: 500")
}

func TestPushArgoApp_Error_HTTPDoFailed(t *testing.T) {
	t.Parallel()

	// While TestPushArgoApp covers status code failures, this covers connection refused/client failures
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()

	oc := &OctantConnection{
		httpClient: ts.Client(),
		argoClient: &mockArgoClient{
			IntegrationData: &integration.ArgoCDIntegrationData{APIUrl: ts.URL},
		},
		datadogClient: &mockDatadogClient{
			IntegrationData: &integration.DataDogIntegrationData{},
		},
	}

	connData := OctantConnectionData{
		Destinations: []OctantConnectionDestination{
			{DestinationType: "datadog", IntegrationName: "dd-1"},
		},
		Deployment: &Deployment{IntegrationName: "argo-test"},
	}

	err := oc.pushArgoApp(context.Background(), "default", "my-test-app", connData)
	require.Error(t, err)
}
