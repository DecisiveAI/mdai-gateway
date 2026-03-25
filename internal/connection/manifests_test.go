package connection

import (
	"encoding/json"
	"testing"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestRenderArgoAppManifest(t *testing.T) {
	oc := &OctantConnection{}

	templateData := ArgoTemplateData{
		AppName:   "test-app",
		Namespace: "team-a-namespace",
	}

	result, err := oc.renderArgoAppManifest(&templateData)
	require.NoError(t, err)
	require.NotEmpty(t, result)

	// Verify the result is valid JSON and contains our injected data
	var parsed map[string]interface{}
	err = json.Unmarshal(result, &parsed)
	require.NoError(t, err, "Rendered output should be valid JSON")

	// Verify App Name
	metadata, ok := parsed["metadata"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "test-app", metadata["name"])

	// Verify Namespace
	spec, ok := parsed["spec"].(map[string]interface{})
	require.True(t, ok)
	destination, ok := spec["destination"].(map[string]interface{})
	require.True(t, ok)
	assert.Equal(t, "team-a-namespace", destination["namespace"])
}

func TestRenderSyncManifests(t *testing.T) {
	oc := &OctantConnection{}

	templateData := ArgoTemplateData{
		AppName:   "test-app",
		Namespace: "default",
		ConnectionData: OctantConnectionData{
			TelemetryTypes: []Telemetry{"logs", "traces"}, // Adjust type if Telemetry is an int/custom alias
		},
		DatadogIntegrationData: &integration.DataDogIntegrationData{
			APIKey: "fake-dd-api-key",
			DDUrl:  "https://datadoghq.com",
		},
		IsArgoSideload: true, // Should trigger annotation injection
	}

	manifests, err := oc.renderSyncManifests(&templateData)
	require.NoError(t, err)

	// Index manifests by their "Kind" and "Name" for easy lookup and assertion
	parsedManifests := make(map[string]map[string]interface{})
	for _, manifestStr := range manifests {
		var parsed map[string]interface{}
		err = json.Unmarshal([]byte(manifestStr), &parsed)
		require.NoError(t, err, "Each sync manifest should be valid JSON")

		kind := parsed["kind"].(string)
		metadata := parsed["metadata"].(map[string]interface{})
		name := metadata["name"].(string)

		key := kind + "/" + name
		parsedManifests[key] = parsed
	}

	t.Run("Secret Rendering", func(t *testing.T) {
		secret, exists := parsedManifests["Secret/test-app-integration-secret"]
		require.True(t, exists, "Secret manifest not found")

		// Verify Argo sideload tracking annotation
		metadata := secret["metadata"].(map[string]interface{})
		annotations := metadata["annotations"].(map[string]interface{})
		assert.Contains(t, annotations["argocd.argoproj.io/tracking-id"], "test-app")

		// Verify Datadog injection
		stringData := secret["stringData"].(map[string]interface{})
		assert.Equal(t, "fake-dd-api-key", stringData["api-key"])
		assert.Equal(t, "https://datadoghq.com", stringData["site-url"])
	})

	t.Run("Primary Collector Rendering", func(t *testing.T) {
		otel, exists := parsedManifests["OpenTelemetryCollector/test-app-primary"]
		require.True(t, exists, "Primary Collector manifest not found")

		// Verify Argo sideload tracking annotation
		metadata := otel["metadata"].(map[string]interface{})
		annotations := metadata["annotations"].(map[string]interface{})
		assert.Contains(t, annotations["argocd.argoproj.io/tracking-id"], "test-app")

		spec := otel["spec"].(map[string]interface{})

		// Verify Env Vars
		envVars := spec["env"].([]interface{})
		require.Len(t, envVars, 2)
		env1 := envVars[0].(map[string]interface{})
		assert.Equal(t, "DD_API_KEY", env1["name"])

		// Verify config string templates out pipelines
		configStr := spec["config"].(string)
		assert.Contains(t, configStr, "logs:")
		assert.Contains(t, configStr, "traces:")
		assert.Contains(t, configStr, "datadog:") // Ensure DD exporter is injected
	})

	t.Run("Envoy Rendering", func(t *testing.T) {
		// Just doing quick existence and name checks for Envoy resources
		_, existsDeploy := parsedManifests["Deployment/test-app-envoy"]
		assert.True(t, existsDeploy, "Envoy Deployment not found")

		_, existsSvc := parsedManifests["Service/test-app-envoy-service"]
		assert.True(t, existsSvc, "Envoy Service not found")

		cm, existsCM := parsedManifests["ConfigMap/test-app-envoy-config"]
		require.True(t, existsCM, "Envoy ConfigMap not found")

		// Check that envoy.yaml data exists
		data := cm["data"].(map[string]interface{})
		assert.Contains(t, data["envoy.yaml"], "primary_collector")
	})
}
