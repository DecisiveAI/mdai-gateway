package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/mydecisive/mdai-gateway/internal/httputil"
	"github.com/mydecisive/mdai-gateway/internal/integration"
	"go.uber.org/zap"
)

type ArgoCDHandler struct {
	argocdIntegration integration.Integration[integration.ArgoCDIntegrationData]
	k8sNamespace      string
	logger            *zap.Logger
}

func NewArgoCDHandler(theIntegration integration.Integration[integration.ArgoCDIntegrationData], k8sNamespace string, logger *zap.Logger) *ArgoCDHandler {
	return &ArgoCDHandler{argocdIntegration: theIntegration, k8sNamespace: k8sNamespace, logger: logger}
}

func (ah *ArgoCDHandler) GetIntegrations(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		argocdIntegrations, err := ah.argocdIntegration.GetIntegrations(ctx, ah.k8sNamespace)
		if err != nil {
			ah.logger.Error("Failed to get integration from Secret", zap.Error(err))
			http.Error(w, "Failed to get integrations", http.StatusInternalServerError)
			return
		}

		integrationList := make([]string, 0, len(argocdIntegrations))
		for intName := range argocdIntegrations {
			integrationList = append(integrationList, intName)
		}
		httputil.WriteJSONResponse(w, ah.logger, http.StatusOK, integrationList)
	}
}

func (ah *ArgoCDHandler) PutIntegrationData(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		integrationName := req.PathValue("integrationName")
		defer func() {
			if err := req.Body.Close(); err != nil {
				ah.logger.Error("Failed to close request body", zap.Error(err))
			}
		}()

		var argocdIntegration integration.ArgoCDIntegrationData
		if err := json.NewDecoder(req.Body).Decode(&argocdIntegration); err != nil {
			ah.logger.Error("request payload was invalid", zap.Error(err))
			http.Error(w, "request payload was invalid", http.StatusBadRequest)
			return
		}

		err := ah.argocdIntegration.SetIntegration(ctx, ah.k8sNamespace, integrationName, argocdIntegration)
		if err != nil {
			ah.logger.Error("failed to update integration", zap.Error(err))
			http.Error(w, "Failed to update integration", http.StatusInternalServerError)
			return
		}
		httputil.WriteJSONResponse(w, ah.logger, http.StatusOK, "")
	}
}

func (ah *ArgoCDHandler) DeleteIntegration(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		integrationName := req.PathValue("integrationName")

		err := ah.argocdIntegration.DeleteIntegration(ctx, ah.k8sNamespace, integrationName)
		if err != nil {
			ah.logger.Error("Failed to delete integration", zap.Error(err))
			http.Error(w, "Failed to delete integration", http.StatusInternalServerError)
			return
		}
		httputil.WriteJSONResponse(w, ah.logger, http.StatusOK, "")
	}
}
