package connection

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"time"

	"github.com/mydecisive/mdai-gateway/internal/integration"
)

type ArgoApp struct {
	Status ArgoAppStatus `json:"status"`
}

type ArgoAppStatus struct {
	Resources []ArgoAppResources `json:"resources"`
	Health    ArgoAppHealth      `json:"health"`
}

type ArgoAppHealth struct {
	Status             string    `json:"status"`
	LastTransitionTime time.Time `json:"lastTransitionTime"`
}

type ArgoAppResources struct {
	Kind string `json:"kind"`
	Name string `json:"name"`
}

func (oc *OctantConnection) getArgoAppStatus(ctx context.Context, name string, namespace string, connection OctantConnectionData) (*ArgoApp, error) {
	argoIntegration, getArgoIntErr := oc.argoClient.GetIntegrationByName(ctx, namespace, connection.Deployment.IntegrationName)
	if getArgoIntErr != nil {
		return nil, getArgoIntErr
	}

	// GET APP
	getAppUrl := fmt.Sprintf("%s/api/v1/applications/%s?upsert=true", argoIntegration.APIUrl, name)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, getAppUrl, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+argoIntegration.AccountToken)
	resp, err := oc.httpClient.Do(req)
	defer resp.Body.Close()
	if err != nil {
		return nil, err
	}
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	var app ArgoApp
	if err := json.NewDecoder(resp.Body).Decode(&app); err != nil {
		return nil, err
	}

	return &app, nil
}

func (oc *OctantConnection) pushArgoApp(ctx context.Context, namespace, name string, connection OctantConnectionData) error {
	if len(connection.Destinations) != 1 {
		// TODO: Implement multiple destination handling and handling of non-dd integrations
		return errors.New("pushing argo application to multiple destinations is currently unsupported")
	}
	var datadogIntegration *integration.DataDogIntegrationData
	for _, destination := range connection.Destinations {
		switch destination.DestinationType {
		case "datadog":
			foundDDIntegration, getDDIntErr := oc.datadogClient.GetIntegrationByName(ctx, namespace, destination.IntegrationName)
			if getDDIntErr != nil {
				return getDDIntErr
			}
			datadogIntegration = foundDDIntegration
		default:
			return fmt.Errorf("unknown destination type: %s", destination.DestinationType)
		}
	}

	argoIntegration, getArgoIntErr := oc.argoClient.GetIntegrationByName(ctx, namespace, connection.Deployment.IntegrationName)
	if getArgoIntErr != nil {
		return getArgoIntErr
	}

	templateData := ArgoTemplateData{
		AppName:                name,
		Namespace:              namespace,
		ConnectionData:         connection,
		DatadogIntegrationData: datadogIntegration,
		// Tells template to manually inject Argo tracking annotations. We only want these for direct sync force push
		IsArgoSideload: true,
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
		"strategy": map[string]any{
			"apply": map[string]bool{
				"force": false,
			},
		},
		"manifests": manifests,
	}
	syncPayloadJson, err := json.Marshal(syncPayload)
	if err != nil {
		return err
	}
	syncUrl := fmt.Sprintf("%s/api/v1/applications/%s/sync", argoIntegration.APIUrl, name)
	syncReq, err := http.NewRequestWithContext(ctx, http.MethodPost, syncUrl, bytes.NewReader(syncPayloadJson))
	if err != nil {
		return err
	}
	syncReq.Header.Set("Content-Type", "application/json")
	syncReq.Header.Set("Authorization", "Bearer "+argoIntegration.AccountToken)
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
	createAppUrl := argoIntegration.APIUrl + "/api/v1/applications"
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, createAppUrl, bytes.NewReader(appJson))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+argoIntegration.AccountToken)
	resp, err := oc.httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}

func (oc *OctantConnection) deleteArgoApp(ctx context.Context, name string, namespace string, connection OctantConnectionData) error {
	argoIntegration, getArgoIntErr := oc.argoClient.GetIntegrationByName(ctx, namespace, connection.Deployment.IntegrationName)
	if getArgoIntErr != nil {
		return getArgoIntErr
	}

	query := "?cascade=true&propagationPolicy=foreground&appNamespace=argocd&cascade=true"
	deleteAppUrl := fmt.Sprintf("%s/api/v1/applications/%s%s", argoIntegration.APIUrl, name, query)
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, deleteAppUrl, nil)
	if err != nil {
		return err
	}
	// Despite no body being required, ArgoCD requires a JSON content type to process Delete
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Authorization", "Bearer "+argoIntegration.AccountToken)
	resp, err := oc.httpClient.Do(req)
	if err != nil {
		return err
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("unexpected status code: %d", resp.StatusCode)
	}
	return nil
}
