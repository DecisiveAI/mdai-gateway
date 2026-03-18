package server

import (
	"context"
	"encoding/json"
	"net/http"

	"github.com/mydecisive/mdai-gateway/internal/connection"
	"github.com/mydecisive/mdai-gateway/internal/httputil"
	"go.uber.org/zap"
)

type ConnectionsHandler struct {
	octantConnection connection.Connection[connection.OctantConnectionData]
	k8sNamespace     string
	logger           *zap.Logger
}

func NewConnectionsHandler(
	octantConnection connection.Connection[connection.OctantConnectionData],
	k8sNamespace string,
	logger *zap.Logger,
) *ConnectionsHandler {
	return &ConnectionsHandler{octantConnection: octantConnection, k8sNamespace: k8sNamespace, logger: logger}
}

func (ch *ConnectionsHandler) GetConnectionByName(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		connectionName := req.PathValue("connectionName")

		theConnection, err := ch.octantConnection.GetConnectionByName(ctx, ch.k8sNamespace, connectionName)
		if err != nil {
			ch.logger.Error("failed to get connection", zap.Error(err))
			http.Error(w, "failed to get connection", http.StatusInternalServerError)
			return
		}

		if theConnection == nil {
			ch.logger.Warn("connection not found", zap.String("connectionName", connectionName))
			http.Error(w, "connection not found", http.StatusNotFound)
			return
		}

		httputil.WriteJSONResponse(w, ch.logger, http.StatusOK, theConnection)
	}
}

func (ch *ConnectionsHandler) SaveConnectionData(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		connectionName := req.PathValue("connectionName")
		defer func() {
			if err := req.Body.Close(); err != nil {
				ch.logger.Error("Failed to close request body", zap.Error(err))
			}
		}()

		var theConnection connection.OctantConnectionData
		if err := json.NewDecoder(req.Body).Decode(&theConnection); err != nil {
			ch.logger.Error("request payload was invalid", zap.Error(err))
			http.Error(w, "request payload was invalid", http.StatusBadRequest)
			return
		}

		err := ch.octantConnection.SaveConnection(ctx, theConnection, ch.k8sNamespace, connectionName)
		if err != nil {
			ch.logger.Error("failed to update connection", zap.Error(err))
			http.Error(w, "Failed to update connection", http.StatusInternalServerError)
			return
		}
		httputil.WriteJSONResponse(w, ch.logger, http.StatusOK, "")
	}
}

func (ch *ConnectionsHandler) DeleteConnectionByName(ctx context.Context) http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		connectionName := req.PathValue("connectionName")

		err := ch.octantConnection.DeleteConnection(ctx, ch.k8sNamespace, connectionName)
		if err != nil {
			ch.logger.Error("Failed to delete connection", zap.Error(err))
			http.Error(w, "Failed to delete connection", http.StatusInternalServerError)
			return
		}
		httputil.WriteJSONResponse(w, ch.logger, http.StatusOK, "")
	}
}
