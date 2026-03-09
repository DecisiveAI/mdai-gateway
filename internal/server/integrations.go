package server

import (
	"context"
	"encoding/json"
	"github.com/decisiveai/mdai-gateway/internal/httputil"
	"github.com/decisiveai/mdai-gateway/internal/integration"
	"go.uber.org/zap"
	"k8s.io/client-go/kubernetes"
	"net/http"
)

type IntegrationsHandler struct {
	k8sClient kubernetes.Interface
}

func NewIntegrationsHandler(k8sClient kubernetes.Interface) *IntegrationsHandler {
	return &IntegrationsHandler{k8sClient: k8sClient}
}

func (ih *IntegrationsHandler) HandleGetIntegrationsOfType(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		integrationTypeStr := req.PathValue("integrationType")
		integrationType := integration.Type(integrationTypeStr)
		switch integrationType {
		case integration.DataDogIntegrationType:
			datadogIntegration := &integration.DataDogIntegration{K8sClient: ih.k8sClient}
			ddInt, err := datadogIntegration.GetIntegrationsFromSecret(ctx, deps.K8sNamespace)
			if err != nil {
				deps.Logger.Error("Failed to get integration from Secret", zap.Error(err))
				http.Error(w, "Failed to get integrations", http.StatusBadRequest)
			}
			var ddIntList []string
			for intName := range ddInt {
				ddIntList = append(ddIntList, intName)
			}
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, ddIntList)
		default:
			http.Error(w, "Invalid integration type", http.StatusBadRequest)
		}
	}
}

func (ih *IntegrationsHandler) HandlePutIntegrationData(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		integrationName := req.PathValue("integrationName")
		integrationTypeStr := req.PathValue("integrationType")
		integrationType := integration.Type(integrationTypeStr)
		defer req.Body.Close()

		switch integrationType {
		case integration.DataDogIntegrationType:
			var ddInt integration.DataDogIntegrationData
			if err := json.NewDecoder(req.Body).Decode(&ddInt); err != nil {
				http.Error(w, "Invalid JSON format in request payload", http.StatusBadRequest)
				return
			}

			datadogIntegration := &integration.DataDogIntegration{K8sClient: ih.k8sClient}
			err := datadogIntegration.SetIntegration(ctx, deps.K8sNamespace, integrationName, ddInt)
			if err != nil {
				http.Error(w, "Failed to update integration", http.StatusInternalServerError)
			}
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, "")
		default:
			http.Error(w, "Invalid integration type", http.StatusBadRequest)
		}
	}
}

func (ih *IntegrationsHandler) HandleDeleteIntegration(ctx context.Context, deps HandlerDeps) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		integrationName := req.PathValue("integrationName")
		integrationTypeStr := req.PathValue("integrationType")
		integrationType := integration.IntegrationType(integrationTypeStr)
		switch integrationType {
		case integration.DataDogIntegrationType:
			err := integration.DeleteIntegration(ctx, deps.K8sClient, deps.K8sNamespace, integrationName, integrationType)
			if err != nil {
				deps.Logger.Error("Failed to get integration from Secret", zap.Error(err))
				http.Error(w, "Failed to update integration", http.StatusInternalServerError)
			}
			httputil.WriteJSONResponse(w, deps.Logger, http.StatusOK, "")
		default:
			http.Error(w, "Invalid integration type", http.StatusBadRequest)
		}
	}
}
