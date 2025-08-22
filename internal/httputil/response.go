package httputil

import (
	"encoding/json"
	"net/http"

	"go.uber.org/zap"
)

type PrometheusAlertResponse struct {
	Message    string `json:"message"`
	Total      int    `json:"total"`
	Successful int    `json:"successful"`
	Skipped    int    `json:"skipped"`
}

func WriteJSONResponse(w http.ResponseWriter, logger *zap.Logger, status int, response any) {
	w.Header().Add("Content-Type", "application/json")
	w.WriteHeader(status)

	if err := json.NewEncoder(w).Encode(response); err != nil {
		logger.Error("failed to write response body: %v", zap.Error(err))
	}
}
