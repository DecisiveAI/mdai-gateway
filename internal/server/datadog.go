package server

import (
	"context"
	"encoding/json"
	"github.com/decisiveai/mdai-gateway/internal/httputil"
	"github.com/decisiveai/mdai-gateway/internal/integration"
	"go.uber.org/zap"
	"net/http"
)

type DatadogHandler struct {
	datadogIntegration integration.Integration
}

func NewDatadogHandler(integration integration.Integration) *DatadogHandler {
	return &DatadogHandler{datadogIntegration: integration}
}

func (dh *DatadogHandler) GetIntegrations(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ddInt, err := dh.datadogIntegration.GetIntegrationsFromSecret(ctx, deps.K8sNamespace)
		if err != nil {
			deps.Logger.Error("Failed to get integration from Secret", zap.Error(err))
			http.Error(w, "Failed to get integrations", http.StatusBadRequest)
		}
		var ddIntList []string
		for intName := range ddInt {
			ddIntList = append(ddIntList, intName)
		}
		httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, ddIntList)
	}
}

func (dh *DatadogHandler) PutIntegrationData(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		integrationName := req.PathValue("integrationName")
		defer req.Body.Close()

		var ddInt integration.DataDogIntegrationData
		if err := json.NewDecoder(req.Body).Decode(&ddInt); err != nil {
			http.Error(w, "request payload was invalid", http.StatusBadRequest)
			return
		}

		err := dh.datadogIntegration.SetIntegration(ctx, deps.K8sNamespace, integrationName, ddInt)
		if err != nil {
			http.Error(w, "Failed to update integration", http.StatusInternalServerError)
		}
		httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, "")
	}
}

func (dh *DatadogHandler) DeleteIntegration(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		integrationName := req.PathValue("integrationName")
		err := dh.datadogIntegration.DeleteIntegration(ctx, deps.K8sNamespace, integrationName)
		if err != nil {
			deps.Logger.Error("Failed to get integration from Secret", zap.Error(err))
			http.Error(w, "Failed to update integration", http.StatusInternalServerError)
		}
		httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, "")
	}
}
