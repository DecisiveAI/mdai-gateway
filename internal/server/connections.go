package server

import (
	"archive/zip"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"slices"
	"time"

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

func (ch *ConnectionsHandler) GenerateManifestsForGivenConnection() http.HandlerFunc {
	return func(w http.ResponseWriter, req *http.Request) {
		defer func() {
			if err := req.Body.Close(); err != nil {
				ch.logger.Error("Failed to close request body", zap.Error(err))
			}
		}()

		connectionName := req.PathValue("connectionName")
		formatStr := req.PathValue("format")
		format := connection.ManifestOutputFormat(formatStr)
		if !slices.Contains(([]connection.ManifestOutputFormat{connection.YAMLOutputFormat, connection.JSONOutputFormat}), format) {
			http.Error(w, fmt.Sprintf("invalid format %s, expected yaml or json", format), http.StatusBadRequest)
			return
		}

		var theConnection connection.OctantConnectionData
		if err := json.NewDecoder(req.Body).Decode(&theConnection); err != nil {
			ch.logger.Error("request payload was invalid", zap.Error(err))
			http.Error(w, "request payload was invalid", http.StatusBadRequest)
			return
		}

		manifestsMap, err := connection.CreateExportableArgoManifests(ch.k8sNamespace, connectionName, theConnection, format)
		if err != nil {
			ch.logger.Error("failed to update connection", zap.Error(err))
			http.Error(w, "Failed to update connection", http.StatusInternalServerError)
			return
		}

		GenerateManifestZipResponse(w, manifestsMap, ch, connectionName)
	}
}

func GenerateManifestZipResponse(w http.ResponseWriter, manifestsMap map[string][]byte, ch *ConnectionsHandler, connectionName string) {
	var buf bytes.Buffer
	zipWriter := zip.NewWriter(&buf)

	for filename, content := range manifestsMap {
		fWriter, err := zipWriter.Create(filename)
		if err != nil {
			ch.logger.Error("failed to create file in zip archive", zap.Error(err), zap.String("filename", filename))
			http.Error(w, "failed to generate zip file", http.StatusInternalServerError)
			return
		}

		if _, err := fWriter.Write(content); err != nil {
			ch.logger.Error("failed to write content to zip archive", zap.Error(err), zap.String("filename", filename))
			http.Error(w, "failed to generate zip file", http.StatusInternalServerError)
			return
		}
	}

	if err := zipWriter.Close(); err != nil {
		ch.logger.Error("failed to close zip writer", zap.Error(err))
		http.Error(w, "failed to finalize zip file", http.StatusInternalServerError)
		return
	}

	w.Header().Set("Content-Type", "application/zip")
	timestamp := time.Now().Format("20060102-150405")
	namePrefix := connectionName
	if namePrefix == "" {
		namePrefix = "octant"
	}
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="%s-manifests-%s.zip"`, namePrefix, timestamp))

	if _, err := w.Write(buf.Bytes()); err != nil {
		ch.logger.Error("failed to send zip file to client", zap.Error(err))
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

		if err := ch.octantConnection.SaveConnection(ctx, theConnection, ch.k8sNamespace, connectionName); err != nil {
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

		if err := ch.octantConnection.DeleteConnection(ctx, ch.k8sNamespace, connectionName); err != nil {
			ch.logger.Error("Failed to delete connection", zap.Error(err))
			http.Error(w, "Failed to delete connection", http.StatusInternalServerError)
			return
		}
		httputil.WriteJSONResponse(w, ch.logger, http.StatusOK, "")
	}
}
