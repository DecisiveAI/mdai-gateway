package connection

import (
	"bytes"
	_ "embed"
	"errors"
	"fmt"
	"text/template"

	"github.com/mydecisive/mdai-gateway/internal/integration"
	"sigs.k8s.io/yaml"
)

//go:embed templates/argo-app.yaml.tmpl
var argoAppTemplate string

//go:embed templates/collector.yaml.tmpl
var primaryCollectorTemplate string

//go:embed templates/validator.yaml.tmpl
var validatorTemplate string

//go:embed templates/secret.yaml.tmpl
var secretTemplate string

type ArgoTemplateData struct {
	AppName                string
	Namespace              string
	ConnectionData         OctantConnectionData
	DatadogIntegrationData *integration.DataDogIntegrationData
	ValidatorEnabled       bool
	IsArgoSideload         bool
}

type ManifestOutputFormat string

const (
	YAMLOutputFormat ManifestOutputFormat = "yaml"
	JSONOutputFormat ManifestOutputFormat = "json"
)

func (*OctantConnection) renderArgoAppManifest(templateData *ArgoTemplateData, outputFormat ManifestOutputFormat) ([]byte, error) {
	if outputFormat == "" {
		return []byte{}, errors.New("no output format specified")
	}
	appManifestTemplate, err := template.New("argo-app").Parse(argoAppTemplate)
	if err != nil {
		return []byte{}, err
	}
	var renderedYaml bytes.Buffer
	if templateErr := appManifestTemplate.Execute(&renderedYaml, templateData); templateErr != nil {
		return []byte{}, templateErr
	}

	switch outputFormat {
	case YAMLOutputFormat:
		return renderedYaml.Bytes(), nil
	case JSONOutputFormat:
		renderedJSON, err := yaml.YAMLToJSON(renderedYaml.Bytes())
		if err != nil {
			return []byte{}, err
		}

		return renderedJSON, nil
	}

	return renderedYaml.Bytes(), nil
}

func (*OctantConnection) renderSyncManifests(templateData *ArgoTemplateData, outputFormat ManifestOutputFormat) (*map[string][]byte, error) {
	if outputFormat == "" {
		return nil, errors.New("no output format specified")
	}

	manifests := make(map[string][]byte)
	for templateName, templateString := range map[string]string{
		"collector": primaryCollectorTemplate,
		"validator": validatorTemplate,
		"secret":    secretTemplate,
	} {
		appManifestTemplate, err := template.New(templateName).Parse(templateString)
		if err != nil {
			return &manifests, err
		}
		var renderedYaml bytes.Buffer
		if templateErr := appManifestTemplate.Execute(&renderedYaml, templateData); templateErr != nil {
			return &manifests, templateErr
		}

		filename := fmt.Sprintf("%s.%s", templateName, outputFormat)
		switch outputFormat {
		case YAMLOutputFormat:
			manifests[filename] = renderedYaml.Bytes()
		case JSONOutputFormat:
			renderedJSON, err := yaml.YAMLToJSON(renderedYaml.Bytes())
			if err != nil {
				return &manifests, err
			}

			manifests[filename] = renderedJSON
		}

	}

	return &manifests, nil
}
