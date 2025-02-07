package main

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"

	"github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
)

func TestMain(m *testing.M) {
	if os.Getenv("SHOW_LOGS") == "1" {
		setupLogger(zap.ErrorLevel)
	} else {
		setupLogger(zap.FatalLevel)
	}
	m.Run()
}

func TestUpdateValkeyHandler1(t *testing.T) {
	ctx := context.Background()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	alertPostBody1, err := os.ReadFile("testdata/alert_post_body_1.json")
	require.NoError(t, err)

	valkeyClient := mock.NewClient(ctrl)

	go processAlertsQueue(ctx, valkeyClient)

	mux := http.NewServeMux()
	mux.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))

	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("XADD", "events", "*", "action", "mdai/add_element", "key", "variable/mdaihub-sample/service_list", "value", "service-a"),
		mock.Match("XADD", "events", "*", "action", "mdai/remove_element", "key", "variable/mdaihub-sample/service_list", "value", "service-b"),
		mock.Match("XADD", "events", "*", "action", "mdai/replace_element", "key", "variable/mdaihub-sample/service_listx", "value", "service-c"),
	).Times(1)

	mockResponses := []valkey.ValkeyResult{
		mock.Result(xReadGroupResponse{id: "1738864789789-0", action: AddElement, key: "variable/mdaihub-sample/service_list", value: "service-a"}.toValkeyMessage()),
		mock.Result(xReadGroupResponse{id: "1738864789789-1", action: RemoveElement, key: "variable/mdaihub-sample/service_list", value: "service-b"}.toValkeyMessage()),
		mock.Result(xReadGroupResponse{id: "1738864789789-2", action: ReplaceValue, key: "variable/mdaihub-sample/service_listx", value: "service-c"}.toValkeyMessage()),
	}

	callCount := 0
	valkeyClient.EXPECT().Do(ctx, mock.MatchFn(func(cmd []string) bool {
		return slices.Equal(cmd, []string{"XREADGROUP", "GROUP", "consumer-group-1", "consumer-1", "BLOCK", "0", "STREAMS", "events", ">"})
	})).DoAndReturn(func(ctx context.Context, cmd valkey.Completed) valkey.ValkeyResult {
		if callCount < len(mockResponses) {
			response := mockResponses[callCount]
			callCount++
			return response
		}
		return mock.Result(mock.ValkeyNil())
	}).AnyTimes()

	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SADD", "variable/mdaihub-sample/service_list", "service-a"),
		mock.Match("XACK", "events", "consumer-group-1", "1738864789789-0"),
	).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-b"),
		mock.Match("XACK", "events", "consumer-group-1", "1738864789789-1"),
	).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SET", "variable/mdaihub-sample/service_listx", "service-c"),
		mock.Match("XACK", "events", "consumer-group-1", "1738864789789-2"),
	).Times(1)

	req := httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody1))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, SuccessResponse, rec.Body.String())
}

func TestUpdateValkeyHandler2(t *testing.T) {
	// one more time with different payload
	ctx := context.Background()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	valkeyClient := mock.NewClient(ctrl)

	alertPostBody2, err := os.ReadFile("testdata/alert_post_body_2.json")
	require.NoError(t, err)

	go processAlertsQueue(ctx, valkeyClient)

	mux := http.NewServeMux()
	mux.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))

	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("XADD", "events", "*", "action", "mdai/remove_element", "key", "variable/mdaihub-sample/service_list", "value", "service-a"),
		mock.Match("XADD", "events", "*", "action", "mdai/remove_element", "key", "variable/mdaihub-sample/service_list", "value", "service-b"),
		mock.Match("XADD", "events", "*", "action", "mdai/remove_element", "key", "variable/mdaihub-sample/service_list", "value", "service-c"),
	).Times(1)

	mockResponses := []valkey.ValkeyResult{
		mock.Result(xReadGroupResponse{id: "1738864789789-0", action: RemoveElement, key: "variable/mdaihub-sample/service_list", value: "service-a"}.toValkeyMessage()),
		mock.Result(xReadGroupResponse{id: "1738864789789-1", action: RemoveElement, key: "variable/mdaihub-sample/service_list", value: "service-b"}.toValkeyMessage()),
		mock.Result(xReadGroupResponse{id: "1738864789789-2", action: RemoveElement, key: "variable/mdaihub-sample/service_list", value: "service-c"}.toValkeyMessage()),
	}

	callCount := 0
	valkeyClient.EXPECT().Do(ctx, mock.MatchFn(func(cmd []string) bool {
		return slices.Equal(cmd, []string{"XREADGROUP", "GROUP", "consumer-group-1", "consumer-1", "BLOCK", "0", "STREAMS", "events", ">"})
	})).DoAndReturn(func(ctx context.Context, cmd valkey.Completed) valkey.ValkeyResult {
		if callCount < len(mockResponses) {
			response := mockResponses[callCount]
			callCount++
			return response
		}
		return mock.Result(mock.ValkeyNil())
	}).AnyTimes()

	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-a"),
		mock.Match("XACK", "events", "consumer-group-1", "1738864789789-0"),
	).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-b"),
		mock.Match("XACK", "events", "consumer-group-1", "1738864789789-1"),
	).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-c"),
		mock.Match("XACK", "events", "consumer-group-1", "1738864789789-2"),
	).Times(1)

	req := httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody2))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, SuccessResponse, rec.Body.String())
}

func TestUpdateValkeyHandler3(t *testing.T) {
	// one more time to emulate a scenario when alert was re-created or renamed
	ctx := context.Background()

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	valkeyClient := mock.NewClient(ctrl)

	alertPostBody3, err := os.ReadFile("testdata/alert_post_body_3.json")
	require.NoError(t, err)
	go processAlertsQueue(ctx, valkeyClient)

	mux := http.NewServeMux()
	mux.HandleFunc("/alerts", handleAlertsPost(ctx, valkeyClient))
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("XADD", "events", "*", "action", "mdai/remove_element", "key", "variable/mdaihub-sample/service_list", "value", "service-a"),
		mock.Match("XADD", "events", "*", "action", "mdai/add_element", "key", "variable/mdaihub-sample/service_list", "value", "service-a"),
	).Times(1)

	mockResponses := []valkey.ValkeyResult{
		mock.Result(xReadGroupResponse{id: "1738864789789-0", action: AddElement, key: "variable/mdaihub-sample/service_list", value: "service-a"}.toValkeyMessage()),
		mock.Result(xReadGroupResponse{id: "1738864789789-1", action: RemoveElement, key: "variable/mdaihub-sample/service_list", value: "service-a"}.toValkeyMessage()),
	}

	callCount := 0
	valkeyClient.EXPECT().Do(ctx, mock.MatchFn(func(cmd []string) bool {
		return slices.Equal(cmd, []string{"XREADGROUP", "GROUP", "consumer-group-1", "consumer-1", "BLOCK", "0", "STREAMS", "events", ">"})
	})).DoAndReturn(func(ctx context.Context, cmd valkey.Completed) valkey.ValkeyResult {
		if callCount < len(mockResponses) {
			response := mockResponses[callCount]
			callCount++
			return response
		}
		return mock.Result(mock.ValkeyNil())
	}).AnyTimes()

	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SADD", "variable/mdaihub-sample/service_list", "service-a"),
		mock.Match("XACK", "events", "consumer-group-1", "1738864789789-0"),
	).Times(1)
	valkeyClient.EXPECT().DoMulti(ctx,
		mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-a"),
		mock.Match("XACK", "events", "consumer-group-1", "1738864789789-1"),
	).Times(1)

	// valkeyClient.EXPECT().Do(ctx, mock.Match("XREADGROUP", "GROUP", "consumer-group-1", "consumer-1", "BLOCK", "0", "STREAMS", "events", ">"))
	// valkeyClient.EXPECT().Do(ctx, mock.Match("SADD", "variable/mdaihub-sample/service_list", "service-a")).Return(mock.Result(mock.ValkeyInt64(1))).Times(1)
	// valkeyClient.EXPECT().Do(ctx, mock.Match("SREM", "variable/mdaihub-sample/service_list", "service-a")).Return(mock.Result(mock.ValkeyInt64(1))).Times(1)

	req := httptest.NewRequest(http.MethodPost, "/alerts", bytes.NewBuffer(alertPostBody3))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, SuccessResponse, rec.Body.String())
}

type xReadGroupResponse struct {
	id     string
	action string
	key    string
	value  string
}

func (r xReadGroupResponse) toValkeyMessage() valkey.ValkeyMessage {
	return mock.ValkeyMap(
		map[string]valkey.ValkeyMessage{
			"events": mock.ValkeyArray(
				mock.ValkeyArray(
					mock.ValkeyBlobString(r.id),
					mock.ValkeyArray(
						mock.ValkeyBlobString("action"),
						mock.ValkeyBlobString(r.action),
						mock.ValkeyBlobString("key"),
						mock.ValkeyBlobString(r.key),
						mock.ValkeyBlobString("value"),
						mock.ValkeyBlobString(r.value),
					),
				),
			),
		},
	)
}
