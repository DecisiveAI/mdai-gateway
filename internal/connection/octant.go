package connection

import (
	"bytes"
	"context"
	_ "embed"
	"encoding/json"
	"fmt"
	"github.com/mydecisive/mdai-gateway/internal/integration"
	"net/http"
	"text/template"

	k8serrors "k8s.io/apimachinery/pkg/api/errors"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/kubernetes"
	"sigs.k8s.io/yaml"
)

//go:embed templates/argo-app.yaml
var argoAppTemplate string

//go:embed templates/primary-collector.yaml
var primaryCollectorTemplate string

//go:embed templates/shadow-collector.yaml
var shadowCollectorTemplate string

//go:embed templates/envoy-config.yaml
var envoyConfigTemplate string

//go:embed templates/envoy-deployment.yaml
var envoyDeploymentTemplate string

//go:embed templates/envoy-service.yaml
var envoyServiceTemplate string

//go:embed templates/secret.yaml
var secretTemplate string

var manifestTemplates = map[string]string{
	"primary-collector": primaryCollectorTemplate,
	"shadow-collector":  shadowCollectorTemplate,
	"envoy-config":      envoyConfigTemplate,
	"envoy-deployment":  envoyDeploymentTemplate,
	"envoy-service":     envoyServiceTemplate,
	"secret":            secretTemplate,
}

// FIXME: Actually wire up all needed fields
type OctantConnectionData struct {
	SourceType     string      `json:"sourceType"`
	TelemetryTypes []Telemetry `json:"telemetryTypes"`
	Deployment     *Deployment `json:"deployment"`
}

// FIXME: Actually wire up all needed fields
type Deployment struct {
	Type   string         `json:"type"`
	Fields map[string]any `json:"fields"`
}

type ArgoDeployment struct {
	Branch string `json:"branch"`
}

var _ Connection[OctantConnectionData] = (*OctantConnection)(nil)

type OctantConnection struct {
	httpClient              *http.Client
	K8sClient               kubernetes.Interface
	ArgoCDIntegrationStuff  integration.ArgoCDIntegration
	DataDogIntegrationStuff integration.DataDogIntegration
}

func NewOctantConnection(httpClient *http.Client, k8sClient kubernetes.Interface) *OctantConnection {
	return &OctantConnection{
		httpClient: httpClient,
		K8sClient:  k8sClient,
		ArgoCDIntegrationStuff: integration.ArgoCDIntegration{
			K8sClient: k8sClient,
		},
		DataDogIntegrationStuff: integration.DataDogIntegration{
			K8sClient: k8sClient,
		},
	}
}

type ArgoTemplateData struct {
	AppName        string
	Namespace      string
	ConnectionData OctantConnectionData
	TempDDAPIKey   string
	TempDDURL      string
}

func (oc *OctantConnection) GetConnectionByName(ctx context.Context, namespace, name string) (*OctantConnectionData, error) {
	configmap, err := oc.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil, nil // nolint: nilnil
		}
		return nil, fmt.Errorf("failed to get configmap %s: %w", connectionsConfigmapName, err)
	}

	if _, ok := configmap.Data[name]; !ok {
		return nil, fmt.Errorf("connection '%s' not found", name)
	}

	var connection OctantConnectionData
	if unmarshalErr := json.Unmarshal([]byte(configmap.Data[name]), &connection); unmarshalErr != nil {
		return nil, fmt.Errorf("failed to unmarshal connection data: %w", unmarshalErr)
	}
	return &connection, nil
}

func (oc *OctantConnection) SaveConnection(ctx context.Context, connection OctantConnectionData, namespace, connectionName string) error {
	jsonData, err := json.Marshal(connection)
	if err != nil {
		return fmt.Errorf("failed to marshal connection data: %w", err)
	}

	cm, err := oc.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	isNotFound := k8serrors.IsNotFound(err)
	if err != nil && !isNotFound {
		return fmt.Errorf("failed to fetch configmap %s: %w", connectionsConfigmapName, err)
	}

	if isNotFound {
		// Create the confmap if it does not exist
		return createConnectionConfigMap(ctx, oc.K8sClient, namespace, connectionsConfigmapName, connectionName, string(jsonData))
	}
	// Update the secret if it already exists
	updateConfigMapErr := updateConfigMapWithConnection(ctx, oc.K8sClient, namespace, cm, connectionName, string(jsonData))
	if updateConfigMapErr != nil {
		return updateConfigMapErr
	}

	return oc.pushArgoApp(ctx, namespace, connectionName, connection)
}

func (oc *OctantConnection) DeleteConnection(ctx context.Context, namespace, connectionName string) error {
	cm, err := oc.K8sClient.CoreV1().ConfigMaps(namespace).Get(ctx, connectionsConfigmapName, metav1.GetOptions{})
	if err != nil {
		if k8serrors.IsNotFound(err) {
			return nil
		}
		return fmt.Errorf("failed to fetch configmap %s: %w", connectionsConfigmapName, err)
	}

	if cm.Data == nil {
		return nil
	}
	if _, exists := cm.Data[connectionName]; !exists {
		return nil
	}

	delete(cm.Data, connectionName)

	_, err = oc.K8sClient.CoreV1().ConfigMaps(namespace).Update(ctx, cm, metav1.UpdateOptions{})
	if err != nil {
		return fmt.Errorf("failed to update configmap %s after deletion: %w", connectionsConfigmapName, err)
	}

	return nil
}

func (oc *OctantConnection) getArgoAppStatus(ctx context.Context, namespace, name string) error {
	// GET APP
	return nil
}

func (oc *OctantConnection) pushArgoApp(ctx context.Context, namespace, name string, connection OctantConnectionData) error {
	// FIXME: Actually wire up the integration name here
	datadawgIntegration, getDDIntErr := oc.DataDogIntegrationStuff.GetIntegrationByName(ctx, namespace, "datadawg")
	if getDDIntErr != nil {
		return getDDIntErr
	}
	argoIntegration, getArgoIntErr := oc.ArgoCDIntegrationStuff.GetIntegrationByName(ctx, namespace, "default-argo-integration")
	if getArgoIntErr != nil {
		return getArgoIntErr
	}

	templateData := ArgoTemplateData{
		AppName:        name,
		Namespace:      namespace,
		ConnectionData: connection,
		TempDDAPIKey:   datadawgIntegration.APIKey,
		TempDDURL:      datadawgIntegration.DDUrl,
	}

	appCreateErr := oc.doArgoAppCreation(ctx, templateData, argoIntegration)
	if appCreateErr != nil {
		return appCreateErr
	}

	syncErr := oc.doArgoAppSync(ctx, templateData, argoIntegration, name)
	if syncErr != nil {
		return syncErr
	}

	return nil
}

func (oc *OctantConnection) doArgoAppSync(ctx context.Context, templateData ArgoTemplateData, argoIntegration *integration.ArgoCDIntegrationData, name string) error {
	manifests, err := oc.renderSyncManifests(&templateData)
	if err != nil {
		return err
	}

	syncPayload := map[string]any{
		"revision": "HEAD",
		"prune":    false,
		"dryRun":   false,
		"strategy": map[string]interface{}{
			"apply": map[string]bool{
				"force": false,
			},
		},
		"manifests": manifests,
	}
	syncPayloadJson, err := json.Marshal(syncPayload)
	syncPayloadStr := string(syncPayloadJson)
	if syncPayloadStr == "erresrlkj" {
		return fmt.Errorf("hehehhhhhh")
	}
	if err != nil {
		return err
	}
	syncUrl := fmt.Sprintf("%s/api/v1/applications/%s/sync", argoIntegration.APIUrl, name)
	syncReq, err := http.NewRequestWithContext(ctx, "POST", syncUrl, bytes.NewReader(syncPayloadJson))
	if err != nil {
		return err
	}
	syncReq.Header.Set("Content-Type", "application/json")
	syncReq.Header.Set("Authorization", fmt.Sprintf("Bearer %s", argoIntegration.AccountToken))
	syncResp, err := oc.httpClient.Do(syncReq)
	if err != nil {
		return err
	}
	if syncResp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", syncResp.StatusCode)
	}
	return nil
}

func (oc *OctantConnection) doArgoAppCreation(ctx context.Context, templateData ArgoTemplateData, argoIntegration *integration.ArgoCDIntegrationData) error {
	appJson, err := oc.renderArgoAppManifest(&templateData)
	if err != nil {
		return err
	}
	createAppUrl := fmt.Sprintf("%s/api/v1/applications", argoIntegration.APIUrl)
	req, err := http.NewRequestWithContext(ctx, "POST", createAppUrl, bytes.NewReader(appJson))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", fmt.Sprintf("Bearer %s", argoIntegration.AccountToken))
	resp, err := oc.httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

func (oc *OctantConnection) deleteArgoApp(ctx context.Context, namespace, name string) error {
	// DELETE APP
	return nil
}

func (oc *OctantConnection) renderArgoAppManifest(templateData *ArgoTemplateData) ([]byte, error) {
	appManifestTemplate, err := template.New("argo-app").Parse(argoAppTemplate)
	if err != nil {
		return []byte{}, err
	}
	var renderedYaml bytes.Buffer
	if err := appManifestTemplate.Execute(&renderedYaml, templateData); err != nil {
		return []byte{}, err
	}

	renderedJson, err := yaml.YAMLToJSON(renderedYaml.Bytes())
	if err != nil {
		return []byte{}, err
	}

	return renderedJson, nil
}

func (oc *OctantConnection) renderSyncManifests(templateData *ArgoTemplateData) ([]string, error) {
	// FIXME: Actually wire up telemetry types; for now just renders all three. Probably need to do more than a string template for the collector configs

	var manifests []string
	for templateName, templateString := range manifestTemplates {
		appManifestTemplate, err := template.New(templateName).Parse(templateString)
		if err != nil {
			return manifests, err
		}
		var renderedYaml bytes.Buffer
		if err := appManifestTemplate.Execute(&renderedYaml, templateData); err != nil {
			return manifests, err
		}

		renderedJson, err := yaml.YAMLToJSON(renderedYaml.Bytes())
		if err != nil {
			return manifests, err
		}

		manifests = append(manifests, string(renderedJson))
	}

	return manifests, nil
}
