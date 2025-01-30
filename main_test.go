package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
)

func TestMain(m *testing.M) {
	if os.Getenv("SHOW_LOGS") == "1" {
		log.SetOutput(os.Stderr)
	} else {
		log.SetOutput(io.Discard)
	}
	m.Run()
}

func TestUpdateValkeyHandler(t *testing.T) {
	const (
		successResponse = `{"success": "variable(s) updated"}`
	)

	ctx := context.TODO()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	valkeyClient := mock.NewClient(ctrl)

	alertPostBody1, err := os.ReadFile("testdata/alert_post_body_1.json")
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))
	valkeyClient.EXPECT().Do(ctx, mock.Match("SADD", "variable/mdaihub-sample/service_list", "service-a")).Return(mock.Result(mock.ValkeyInt64(1))).Times(1)
	valkeyClient.EXPECT().Do(ctx, mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-b")).Return(mock.Result(mock.ValkeyInt64(1))).Times(1)
	valkeyClient.EXPECT().Do(ctx, mock.Match("SET", "variable/mdaihub-sample/service_list", "service-c")).Return(mock.Result(mock.ValkeyString("OK"))).Times(0)

	req := httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody1))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, successResponse, rec.Body.String())
}
