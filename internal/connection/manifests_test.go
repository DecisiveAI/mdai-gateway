package connection

import (
	"encoding/json"
	"fmt"
	"testing"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"sigs.k8s.io/yaml"
)

func getNestedField(m map[string]any, keys ...string) (any, bool) {
	var current any = m
	for i, key := range keys {
		currentMap, ok := current.(map[string]any)
		if !ok {
			return nil, false
		}

		val, exists := currentMap[key]
		if !exists {
			return nil, false
		}

		if i == len(keys)-1 {
			return val, true
		}

		current = val
	}
	return nil, false
}

func TestRenderArgoAppManifest(t *testing.T) {
	templateData := ArgoTemplateData{
		AppName:   "test-app",
		Namespace: "team-a-namespace",
	}

	result, err := renderArgoAppManifest(&templateData, JSONOutputFormat)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	var parsed map[string]any
	err = json.Unmarshal(result, &parsed)
	require.NoError(t, err, "Rendered output should be valid JSON")

	metadata, ok := parsed["metadata"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "test-app", metadata["name"])

	spec, ok := parsed["spec"].(map[string]any)
	require.True(t, ok)
	destination, ok := spec["destination"].(map[string]any)
	require.True(t, ok)
	assert.Equal(t, "team-a-namespace", destination["namespace"])
}

func TestRenderSyncManifests(t *testing.T) {
	tests := []struct {
		name         string
		templateData ArgoTemplateData
	}{
		{
			name: "Full Configuration (Datadog, ArgoSideload, Multiple Telemetry Types)",
			templateData: ArgoTemplateData{
				AppName:   "test-app",
				Namespace: "default",
				ConnectionData: OctantConnectionData{
					TelemetryTypes: []Telemetry{"logs", "traces"},
				},
				DatadogIntegrationData: &integration.DataDogIntegrationData{
					APIKey: "fake-dd-api-key",
					DDUrl:  "https://datadoghq.com",
				},
				IsArgoSideload: true,
			},
		},
		{
			name: "Minimal Configuration (No Datadog, No Sideload, No Telemetry)",
			templateData: ArgoTemplateData{
				AppName:   "minimal-app",
				Namespace: "default",
				ConnectionData: OctantConnectionData{
					TelemetryTypes: []Telemetry{},
				},
				DatadogIntegrationData: nil,
				IsArgoSideload:         false,
			},
		},
		{
			name: "Partial Configuration (Traces only, Datadog present, No Sideload)",
			templateData: ArgoTemplateData{
				AppName:   "partial-app",
				Namespace: "default",
				ConnectionData: OctantConnectionData{
					TelemetryTypes: []Telemetry{"traces"},
				},
				DatadogIntegrationData: &integration.DataDogIntegrationData{
					APIKey: "fake-dd-api-key-2",
					DDUrl:  "https://datadoghq.com",
				},
				IsArgoSideload: false,
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			manifests, err := renderCollectorDeploymentManifests(&tt.templateData, JSONOutputFormat)
			require.NoError(t, err)

			parsedManifests := make(map[string]map[string]any)
			for _, manifestStr := range *manifests {
				var parsed map[string]any
				err = json.Unmarshal([]byte(manifestStr), &parsed)
				require.NoError(t, err, "Each sync manifest should be valid JSON")

				kind := parsed["kind"].(string)
				metadata := parsed["metadata"].(map[string]any)
				name := metadata["name"].(string)

				key := fmt.Sprintf("%s/%s", kind, name)
				parsedManifests[key] = parsed
			}

			appName := tt.templateData.AppName

			secretKey := fmt.Sprintf("Secret/%s-integration-secret", appName)
			secret, exists := parsedManifests[secretKey]
			require.True(t, exists, "Secret manifest not found")

			secretMeta := secret["metadata"].(map[string]any)
			annotations, hasAnnotations := secretMeta["annotations"].(map[string]any)

			if tt.templateData.IsArgoSideload {
				require.True(t, hasAnnotations, "Annotations should exist when IsArgoSideload is true")
				assert.Contains(t, annotations["argocd.argoproj.io/tracking-id"], appName)
			} else {
				assert.False(t, hasAnnotations, "Annotations should not exist when IsArgoSideload is false")
			}

			stringData, hasStringData := secret["stringData"].(map[string]any)
			if tt.templateData.DatadogIntegrationData != nil {
				require.True(t, hasStringData, "stringData should exist when DatadogIntegrationData is provided")
				assert.Equal(t, tt.templateData.DatadogIntegrationData.APIKey, stringData["api-key"])
				assert.Equal(t, tt.templateData.DatadogIntegrationData.DDUrl, stringData["site-url"])
			}

			otelKey := fmt.Sprintf("OpenTelemetryCollector/%s", appName)
			otel, exists := parsedManifests[otelKey]
			require.True(t, exists, "Primary Collector manifest not found")

			spec := otel["spec"].(map[string]any)
			configStr := spec["config"].(string)

			var otelConfig map[string]any
			err = yaml.Unmarshal([]byte(configStr), &otelConfig)
			require.NoError(t, err, "OpenTelemetry config should be valid YAML")

			_, hasEnv := spec["env"].([]any)
			if tt.templateData.DatadogIntegrationData != nil {
				require.True(t, hasEnv, "Env block should exist for Datadog integration")

				// Assert on actual values inside the OTel config
				apiBlock, found := getNestedField(otelConfig, "exporters", "datadog", "api")
				require.True(t, found, "Datadog API exporter should be configured")

				apiMap := apiBlock.(map[string]any)
				assert.Equal(t, "${env:DD_API_KEY}", apiMap["key"], "Should reference DD_API_KEY environment variable")
				assert.Equal(t, "${env:DD_SITE}", apiMap["site"], "Should reference DD_SITE environment variable")
			} else {
				assert.False(t, hasEnv, "Env block should be omitted if DatadogIntegrationData is nil")

				_, found := getNestedField(otelConfig, "exporters", "datadog", "api")
				assert.False(t, found, "Datadog API exporter should NOT be configured")
			}

			// Verify dynamic pipelines
			if len(tt.templateData.ConnectionData.TelemetryTypes) > 0 {
				for _, tel := range tt.templateData.ConnectionData.TelemetryTypes {
					receivers, found := getNestedField(otelConfig, "service", "pipelines", string(tel), "receivers")
					require.True(t, found, "Pipeline %s should exist", tel)

					// We can now cast the result and assert exactly what it contains
					recSlice := receivers.([]any)
					assert.Contains(t, recSlice, "datadog", "Pipeline should include datadog receiver")
				}
			} else {
				_, foundLogs := getNestedField(otelConfig, "service", "pipelines", "logs")
				assert.False(t, foundLogs, "Logs pipeline should not exist")

				_, foundTraces := getNestedField(otelConfig, "service", "pipelines", "traces")
				assert.False(t, foundTraces, "Traces pipeline should not exist")
			}

			_, existsDeploy := parsedManifests[fmt.Sprintf("Deployment/%s-envoy", appName)]
			assert.True(t, existsDeploy, "Envoy Deployment not found")

			_, existsSvc := parsedManifests[fmt.Sprintf("Service/%s-envoy-service", appName)]
			assert.True(t, existsSvc, "Envoy Service not found")
		})
	}
}
