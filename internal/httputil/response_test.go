package httputil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestWriteJSONResponse(t *testing.T) {
	rr := httptest.NewRecorder()
	logger := zap.NewNop()

	resp := PrometheusAlertResponse{
		Message:    "ok",
		Total:      3,
		Successful: 3,
	}

	WriteJSONResponse(rr, logger, http.StatusOK, resp)

	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))
	assert.Equal(t, http.StatusOK, rr.Code)

	var parsed PrometheusAlertResponse
	err := json.Unmarshal(rr.Body.Bytes(), &parsed)
	require.NoError(t, err)

	assert.Equal(t, resp, parsed)
}

func TestWriteJSONResponse_ErrorEncoding(t *testing.T) {
	core, observed := observer.New(zap.ErrorLevel)
	logger := zap.New(core)

	rr := httptest.NewRecorder()

	invalid := map[string]any{
		"bad": make(chan int),
	}

	WriteJSONResponse(rr, logger, http.StatusTeapot, invalid) // arbitrary status

	assert.Equal(t, http.StatusTeapot, rr.Code)
	assert.Equal(t, "application/json", rr.Header().Get("Content-Type"))

	logs := observed.All()
	require.Len(t, logs, 1)

	assert.Equal(t, "failed to write response body: %v", logs[0].Message)
	assert.Contains(t, logs[0].ContextMap()["error"].(string), "json: unsupported type: chan int")
}
