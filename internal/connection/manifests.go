package connection

import (
	"bytes"
	_ "embed"
	"github.com/mydecisive/mdai-gateway/internal/integration"
	"sigs.k8s.io/yaml"
	"text/template"
)

//go:embed templates/argo-app.yaml
var argoAppTemplate string

//go:embed templates/primary-collector.yaml
var primaryCollectorTemplate string

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
	"envoy-config":      envoyConfigTemplate,
	"envoy-deployment":  envoyDeploymentTemplate,
	"envoy-service":     envoyServiceTemplate,
	"secret":            secretTemplate,
}

type ArgoTemplateData struct {
	AppName                string
	Namespace              string
	ConnectionData         OctantConnectionData
	DatadogIntegrationData *integration.DataDogIntegrationData
	IsArgoSideload         bool
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
