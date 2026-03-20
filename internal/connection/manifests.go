package connection

import (
	"bytes"
	_ "embed"
	"sigs.k8s.io/yaml"
	"text/template"
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

type ArgoTemplateData struct {
	AppName        string
	Namespace      string
	ConnectionData OctantConnectionData
	TempDDAPIKey   string
	TempDDURL      string
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
