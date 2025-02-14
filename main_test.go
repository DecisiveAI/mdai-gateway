package main

import (
	"bytes"
	"context"
	"io"
	"log"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
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
	alertPostBody2, err := os.ReadFile("testdata/alert_post_body_2.json")
	require.NoError(t, err)
	alertPostBody3, err := os.ReadFile("testdata/alert_post_body_3.json")
	require.NoError(t, err)

	mux := http.NewServeMux()
	mux.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SADD", "variable/mdaihub-sample/service_list", "service-a"),
		mock.MatchFn(func(cmd []string) bool {
			return cmd[0] == "XADD" && cmd[1] == "mdai_hub_event_history" && slices.Contains(cmd, "mdai/add_element") && slices.Contains(cmd, "service-a")
		}),
	).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-b"),
		mock.MatchFn(func(cmd []string) bool {
			return cmd[0] == "XADD" && cmd[1] == "mdai_hub_event_history" && slices.Contains(cmd, "mdai/remove_element") && slices.Contains(cmd, "service-b")
		}),
	).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SET", "variable/mdaihub-sample/service_list", "service-c"),
		mock.MatchFn(func(cmd []string) bool {
			return cmd[0] == "XADD" && cmd[1] == "mdai_hub_event_history" && slices.Contains(cmd, "mdai/replace_element") && slices.Contains(cmd, "service-a")
		}),
	).Times(1)

	req := httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody1))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, successResponse, rec.Body.String())

	// one more time with different payload
	mux = http.NewServeMux()
	mux.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))

	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-a"),
		mock.MatchFn(func(cmd []string) bool {
			return cmd[0] == "XADD" && cmd[1] == "mdai_hub_event_history" && slices.Contains(cmd, "mdai/remove_element") && slices.Contains(cmd, "service-a")
		}),
	).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-b"),
		mock.MatchFn(func(cmd []string) bool {
			return cmd[0] == "XADD" && cmd[1] == "mdai_hub_event_history" && slices.Contains(cmd, "mdai/remove_element") && slices.Contains(cmd, "service-b")
		}),
	).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-c"),
		mock.MatchFn(func(cmd []string) bool {
			return cmd[0] == "XADD" && cmd[1] == "mdai_hub_event_history" && slices.Contains(cmd, "mdai/remove_element") && slices.Contains(cmd, "service-c")
		}),
	).Times(1)

	req = httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody2))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, successResponse, rec.Body.String())

	// one more time to emulate a scenario when alert was re-created or renamed
	mux = http.NewServeMux()
	mux.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))

	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SADD", "variable/mdaihub-sample/service_list", "service-a"),
		mock.MatchFn(func(cmd []string) bool {
			return cmd[0] == "XADD" && cmd[1] == "mdai_hub_event_history" && slices.Contains(cmd, "mdai/remove_element") && slices.Contains(cmd, "service-a")
		}),
	).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-a"),
		mock.MatchFn(func(cmd []string) bool {
			return cmd[0] == "XADD" && cmd[1] == "mdai_hub_event_history" && slices.Contains(cmd, "mdai/remove_element") && slices.Contains(cmd, "service-a")
		}),
	).Times(1)

	req = httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody3))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, successResponse, rec.Body.String())
}
