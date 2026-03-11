package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/mydecisive/mdai-gateway/internal/httputil"
	"github.com/mydecisive/mdai-gateway/internal/integration"
	"go.uber.org/zap"
)

type DatadogHandler struct {
	datadogIntegration integration.Integration
	k8sNamespace       string
	logger             *zap.Logger
}

func NewDatadogHandler(theIntegration integration.Integration, k8sNamespace string, logger *zap.Logger) *DatadogHandler {
	return &DatadogHandler{datadogIntegration: theIntegration, k8sNamespace: k8sNamespace, logger: logger}
}

func (dh *DatadogHandler) GetIntegrations(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		ddInt, err := dh.datadogIntegration.GetIntegrations(ctx, dh.k8sNamespace)
		if err != nil {
			dh.logger.Error("Failed to get integration from Secret", zap.Error(err))
			http.Error(w, "Failed to get integrations", http.StatusInternalServerError)
			return
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
		defer func() {
			if err := req.Body.Close(); err != nil {
				dh.logger.Error("Failed to close request body", zap.Error(err))
			}
		}()

		var ddInt integration.DataDogIntegrationData
		if err := json.NewDecoder(req.Body).Decode(&ddInt); err != nil {
			dh.logger.Error("request payload was invalid", zap.Error(err))
			http.Error(w, "request payload was invalid", http.StatusBadRequest)
			return
		}

		err := dh.datadogIntegration.SetIntegration(ctx, dh.k8sNamespace, integrationName, ddInt)
		if err != nil {
			dh.logger.Error("failed to update integration", zap.Error(err))
			http.Error(w, "Failed to update integration", http.StatusInternalServerError)
			return
		}
		httputil.WriteJSONResponse(w, dh.logger, http.StatusOK, "")
	}
}

func (dh *DatadogHandler) DeleteIntegration(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		integrationName := req.PathValue("integrationName")

		err := dh.datadogIntegration.DeleteIntegration(ctx, dh.k8sNamespace, integrationName)
		if err != nil {
			dh.logger.Error("Failed to delete integration", zap.Error(err))
			http.Error(w, "Failed to delete integration", http.StatusInternalServerError)
			return
		}
		httputil.WriteJSONResponse(w, dh.logger, http.StatusOK, "")
	}
}
