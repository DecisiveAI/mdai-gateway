package connection

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	integrationmock "github.com/mydecisive/mdai-gateway/internal/mock/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

			mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
			mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(&integration.ArgoCDIntegrationData{
				APIUrl:       ts.URL,
				AccountToken: "fake-token",
			}, nil)

			oc := &OctantConnection{
				httpClient: ts.Client(),
				argoClient: mockArgo,
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

	mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
	mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(&integration.ArgoCDIntegrationData{
		APIUrl: ts.URL,
	}, nil)

	oc := &OctantConnection{
		httpClient: ts.Client(),
		argoClient: mockArgo,
	}

	err := oc.deleteArgoApp(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})
	require.NoError(t, err)
}

func TestPushArgoApp(t *testing.T) { // nolint:gocognit
	t.Parallel()

	tests := []struct {
		name               string
		destinations       []OctantConnectionDestination
		expectDatadogCall  bool
		ddClientErr        error
		expectArgoCall     bool
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
			expectDatadogCall: false,
			expectArgoCall:    false,
			expectedErr:       "pushing argo application with multiple destinations is currently unsupported",
		},
		{
			name: "unknown destination type",
			destinations: []OctantConnectionDestination{
				{DestinationType: "newrelic", IntegrationName: "nr-1"},
			},
			expectDatadogCall: false,
			expectArgoCall:    false,
			expectedErr:       "unknown destination type: newrelic",
		},
		{
			name: "datadog integration fetch fails",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
			},
			expectDatadogCall: true,
			ddClientErr:       errors.New("datadog integration not found"),
			expectArgoCall:    false,
			expectedErr:       "datadog integration not found",
		},
		{
			name: "argo integration fetch fails",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
			},
			expectDatadogCall: true,
			expectArgoCall:    true,
			argoClientErr:     errors.New("argo integration not found"),
			expectedErr:       "argo integration not found",
		},
		{
			name: "app creation HTTP call fails",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
			},
			expectDatadogCall:  true,
			expectArgoCall:     true,
			createResponseCode: http.StatusInternalServerError,
			expectedErr:        "unexpected status code: 500",
		},
		{
			name: "app sync HTTP call fails",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
			},
			expectDatadogCall:  true,
			expectArgoCall:     true,
			createResponseCode: http.StatusOK,
			syncResponseCode:   http.StatusBadRequest,
			expectedErr:        "unexpected status code: 400",
		},
		{
			name: "success path",
			destinations: []OctantConnectionDestination{
				{DestinationType: "datadog", IntegrationName: "dd-1"},
			},
			expectDatadogCall:  true,
			expectArgoCall:     true,
			createResponseCode: http.StatusOK,
			syncResponseCode:   http.StatusOK,
			expectedErr:        "",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications" {
					w.WriteHeader(tc.createResponseCode)
					return
				}
				if r.Method == http.MethodPost && r.URL.Path == "/api/v1/applications/my-test-app/sync" {
					w.WriteHeader(tc.syncResponseCode)
					return
				}
				w.WriteHeader(http.StatusOK)
			}))
			defer ts.Close()

			mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
			if tc.expectArgoCall {
				if tc.argoClientErr != nil {
					mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(nil, tc.argoClientErr)
				} else {
					mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(&integration.ArgoCDIntegrationData{
						APIUrl:       ts.URL,
						AccountToken: "fake-token",
					}, nil)
				}
			}

			mockDatadog := integrationmock.NewMockIntegration[integration.DataDogIntegrationData](t)
			if tc.expectDatadogCall {
				if tc.ddClientErr != nil {
					mockDatadog.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "dd-1").Return(nil, tc.ddClientErr)
				} else {
					mockDatadog.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "dd-1").Return(&integration.DataDogIntegrationData{}, nil)
				}
			}

			oc := &OctantConnection{
				httpClient:    ts.Client(),
				argoClient:    mockArgo,
				datadogClient: mockDatadog,
			}

			connData := OctantConnectionData{
				Destinations: tc.destinations,
				Deployment: &Deployment{
					IntegrationName: "argo-test",
				},
			}

			err := oc.pushArgoApp(context.Background(), "default", "my-test-app", connData)

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

	mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
	mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(nil, errors.New("injected argo integration error"))

	oc := &OctantConnection{
		argoClient: mockArgo,
	}

	_, err := oc.getArgoAppStatus(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "injected argo integration error")
}

func TestGetArgoAppStatus_Error_RequestCreation(t *testing.T) {
	t.Parallel()

	mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
	mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(&integration.ArgoCDIntegrationData{
		APIUrl: "://invalid-url",
	}, nil)

	oc := &OctantConnection{
		argoClient: mockArgo,
	}

	_, err := oc.getArgoAppStatus(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
}

func TestGetArgoAppStatus_Error_HTTPDoFailed(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()

	mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
	mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(&integration.ArgoCDIntegrationData{
		APIUrl: ts.URL,
	}, nil)

	oc := &OctantConnection{
		httpClient: ts.Client(),
		argoClient: mockArgo,
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
		w.Write([]byte(`{ "invalid": json `)) // nolint: errcheck,gosec,revive
	}))
	defer ts.Close()

	mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
	mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(&integration.ArgoCDIntegrationData{
		APIUrl: ts.URL,
	}, nil)

	oc := &OctantConnection{
		httpClient: ts.Client(),
		argoClient: mockArgo,
	}

	_, err := oc.getArgoAppStatus(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
}

func TestDeleteArgoApp_Error_IntegrationFetchFailed(t *testing.T) {
	t.Parallel()

	mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
	mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(nil, errors.New("injected argo integration error"))

	oc := &OctantConnection{
		argoClient: mockArgo,
	}

	err := oc.deleteArgoApp(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "injected argo integration error")
}

func TestDeleteArgoApp_Error_RequestCreation(t *testing.T) {
	t.Parallel()

	mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
	mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(&integration.ArgoCDIntegrationData{
		APIUrl: "://invalid-url",
	}, nil)

	oc := &OctantConnection{
		argoClient: mockArgo,
	}

	err := oc.deleteArgoApp(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
}

func TestDeleteArgoApp_Error_HTTPDoFailed(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()

	mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
	mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(&integration.ArgoCDIntegrationData{
		APIUrl: ts.URL,
	}, nil)

	oc := &OctantConnection{
		httpClient: ts.Client(),
		argoClient: mockArgo,
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

	mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
	mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(&integration.ArgoCDIntegrationData{
		APIUrl: ts.URL,
	}, nil)

	oc := &OctantConnection{
		httpClient: ts.Client(),
		argoClient: mockArgo,
	}

	err := oc.deleteArgoApp(context.Background(), "my-app", "default", OctantConnectionData{
		Deployment: &Deployment{IntegrationName: "argo-test"},
	})

	require.Error(t, err)
	assert.Contains(t, err.Error(), "unexpected status code: 500")
}

func TestPushArgoApp_Error_HTTPDoFailed(t *testing.T) {
	t.Parallel()

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	ts.Close()

	mockArgo := integrationmock.NewMockIntegration[integration.ArgoCDIntegrationData](t)
	mockArgo.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "argo-test").Return(&integration.ArgoCDIntegrationData{
		APIUrl: ts.URL,
	}, nil)

	mockDatadog := integrationmock.NewMockIntegration[integration.DataDogIntegrationData](t)
	mockDatadog.EXPECT().GetIntegrationByName(mock.Anything, defaultNamespace, "dd-1").Return(&integration.DataDogIntegrationData{}, nil)

	oc := &OctantConnection{
		httpClient:    ts.Client(),
		argoClient:    mockArgo,
		datadogClient: mockDatadog,
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
