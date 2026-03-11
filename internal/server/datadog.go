package server

import (
	"context"
	"encoding/json"
	"github.com/mydecisive/mdai-gateway/internal/httputil"
	"github.com/mydecisive/mdai-gateway/internal/integration"
	"go.uber.org/zap"
	"net/http"
)

type DatadogHandler struct {
	datadogIntegration integration.Integration
	k8sNamespace       string
	logger             *zap.Logger
}

func NewDatadogHandler(integration integration.Integration, k8sNamespace string, logger *zap.Logger) *DatadogHandler {
	return &DatadogHandler{datadogIntegration: integration, k8sNamespace: k8sNamespace, logger: logger}
}

func (dh *DatadogHandler) GetIntegrations(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ddInt, err := dh.datadogIntegration.GetIntegrations(ctx, dh.k8sNamespace)
		if err != nil {
			dh.logger.Error("Failed to get integration from Secret", zap.Error(err))
			http.Error(w, "Failed to get integrations", http.StatusBadRequest)
		}

		integrationList := make([]string, 0, len(ddInt))
		for intName := range ddInt {
			integrationList = append(integrationList, intName)
		}
		httputil.WriteJSONResponse(w, dh.logger, http.StatusOK, integrationList)
	}
}

func (dh *DatadogHandler) PutIntegrationData(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		integrationName := req.PathValue("integrationName")
		defer req.Body.Close()

		var ddInt integration.DataDogIntegrationData
		if err := json.NewDecoder(req.Body).Decode(&ddInt); err != nil {
			http.Error(w, "request payload was invalid", http.StatusBadRequest)
			return
		}

		err := dh.datadogIntegration.SetIntegration(ctx, dh.k8sNamespace, integrationName, ddInt)
		if err != nil {
			http.Error(w, "Failed to update integration", http.StatusInternalServerError)
		}
		httputil.WriteJSONResponse(w, dh.logger, http.StatusOK, "")
	}
}

func (dh *DatadogHandler) DeleteIntegration(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		integrationName := req.PathValue("integrationName")
		err := dh.datadogIntegration.DeleteIntegration(ctx, dh.k8sNamespace, integrationName)
		if err != nil {
			dh.logger.Error("Failed to get integration from Secret", zap.Error(err))
			http.Error(w, "Failed to update integration", http.StatusInternalServerError)
		}
		httputil.WriteJSONResponse(w, dh.logger, http.StatusOK, "")
	}
}
