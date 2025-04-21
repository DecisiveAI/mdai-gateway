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

	"github.com/valkey-io/valkey-go"

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

type XaddMatcher struct {
	operation  string
	labelValue string
}

func (xadd XaddMatcher) Matches(x any) bool {
	if cmd, ok := x.(valkey.Completed); ok {
		commands := cmd.Commands()
		return slices.Contains(commands, "XADD") && slices.Contains(commands, "mdai_hub_event_history") && (xadd.operation == "" || slices.Contains(commands, xadd.operation)) && slices.Contains(commands, xadd.labelValue)
	}
	return false
}

func (xadd XaddMatcher) String() string {
	return "Wanted XADD to mdai_hub_event_history command with " + xadd.operation + " and " + xadd.labelValue
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

	valkeyClient.EXPECT().Do(ctx,
		XaddMatcher{labelValue: "service-a"},
	).Return(mock.Result(mock.ValkeyString(""))).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SADD", "variable/mdaihub-sample/service_list", "service-a"),
		XaddMatcher{operation: "mdai/add_element", labelValue: "service-a"},
	).Return([]valkey.ValkeyResult{mock.Result(mock.ValkeyInt64(1)), mock.Result(mock.ValkeyString(""))}).Times(1)
	valkeyClient.EXPECT().Do(ctx,
		XaddMatcher{labelValue: "service-b"},
	).Return(mock.Result(mock.ValkeyString(""))).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-b"),
		XaddMatcher{operation: "mdai/remove_element", labelValue: "service-b"},
	).Return([]valkey.ValkeyResult{mock.Result(mock.ValkeyInt64(1)), mock.Result(mock.ValkeyString(""))}).Times(1)
	valkeyClient.EXPECT().Do(ctx,
		XaddMatcher{labelValue: "service-c"},
	).Return(mock.Result(mock.ValkeyString(""))).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SET", "variable/mdaihub-sample/service_list", "service-c"),
		XaddMatcher{operation: "mdai/set", labelValue: "service-c"},
	).Return([]valkey.ValkeyResult{mock.Result(mock.ValkeyInt64(1)), mock.Result(mock.ValkeyString(""))}).Times(1)

	req := httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody1))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, successResponse, rec.Body.String())

	// one more time with different payload
	mux = http.NewServeMux()
	mux.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))

	valkeyClient.EXPECT().Do(ctx,
		XaddMatcher{labelValue: "service-a"},
	).Return(mock.Result(mock.ValkeyString(""))).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-a"),
		XaddMatcher{operation: "mdai/remove_element", labelValue: "service-a"},
	).Return([]valkey.ValkeyResult{mock.Result(mock.ValkeyInt64(1)), mock.Result(mock.ValkeyString(""))}).Times(1)
	valkeyClient.EXPECT().Do(ctx,
		XaddMatcher{labelValue: "service-b"},
	).Return(mock.Result(mock.ValkeyString(""))).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-b"),
		XaddMatcher{operation: "mdai/remove_element", labelValue: "service-b"},
	).Return([]valkey.ValkeyResult{mock.Result(mock.ValkeyInt64(1)), mock.Result(mock.ValkeyString(""))}).Times(1)
	valkeyClient.EXPECT().Do(ctx,
		XaddMatcher{labelValue: "service-c"},
	).Return(mock.Result(mock.ValkeyString(""))).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-c"),
		XaddMatcher{operation: "mdai/remove_element", labelValue: "service-c"},
	).Return([]valkey.ValkeyResult{mock.Result(mock.ValkeyInt64(1)), mock.Result(mock.ValkeyString(""))}).Times(1)

	req = httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody2))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, successResponse, rec.Body.String())

	// one more time to emulate a scenario when alert was re-created or renamed
	mux = http.NewServeMux()
	mux.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))

	valkeyClient.EXPECT().Do(ctx,
		XaddMatcher{labelValue: "service-a"},
	).Return(mock.Result(mock.ValkeyString(""))).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SADD", "variable/mdaihub-sample/service_list", "service-a"),
		XaddMatcher{operation: "mdai/add_element", labelValue: "service-a"},
	).Return([]valkey.ValkeyResult{mock.Result(mock.ValkeyInt64(1)), mock.Result(mock.ValkeyString(""))}).Times(1)
	valkeyClient.EXPECT().Do(ctx,
		XaddMatcher{labelValue: "service-a"},
	).Return(mock.Result(mock.ValkeyString(""))).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-a"),
		XaddMatcher{operation: "mdai/remove_element", labelValue: "service-a"},
	).Return([]valkey.ValkeyResult{mock.Result(mock.ValkeyInt64(1)), mock.Result(mock.ValkeyString(""))}).Times(1)

	req = httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody3))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, successResponse, rec.Body.String())
}
