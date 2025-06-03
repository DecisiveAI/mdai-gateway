package main

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"os"
	"slices"
	"testing"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/decisiveai/mdai-gateway/internal/gateway"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap/zaptest"
)

type XaddMatcher struct {
	operation  string
	labelValue string
}

func (xadd XaddMatcher) Matches(x any) bool {
	if cmd, ok := x.(valkey.Completed); ok {
		commands := cmd.Commands()
		return slices.Contains(commands, "XADD") &&
			slices.Contains(commands, "mdai_hub_event_history") &&
			(xadd.operation == "" || slices.Contains(commands, xadd.operation)) &&
			slices.Contains(commands, xadd.labelValue)
	}
	return false
}

func (xadd XaddMatcher) String() string {
	return "Wanted XADD to mdai_hub_event_history command with " + xadd.operation + " and " + xadd.labelValue
}

func TestUpdateEventsHandler(t *testing.T) {
	const (
		post1Response = `{"message":"Processed Prometheus alerts","successful":6,"total":6}
`
		post2Response = `{"message":"Processed Prometheus alerts","successful":3,"total":3}
`
		post3Response = `{"message":"Processed Prometheus alerts","successful":2,"total":2}
`
	)

	ctx := t.Context()
	logger := zaptest.NewLogger(t)

	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	valkeyClient := mock.NewClient(ctrl)

	alertPostBody1, err := os.ReadFile("../../testdata/alert_post_body_1.json")
	require.NoError(t, err)
	alertPostBody2, err := os.ReadFile("../../testdata/alert_post_body_2.json")
	require.NoError(t, err)
	alertPostBody3, err := os.ReadFile("../../testdata/alert_post_body_3.json")
	require.NoError(t, err)

	mux := http.NewServeMux()
	hub := eventing.NewMockEventHub()
	mux.HandleFunc(eventsEndpoint, gateway.HandleEventsRoute(ctx, valkeyClient, hub, logger))

	req := httptest.NewRequest(http.MethodPost, eventsEndpoint, bytes.NewBuffer(alertPostBody1))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.JSONEq(t, post1Response, rec.Body.String())

	// one more time with different payload
	mux = http.NewServeMux()
	mux.HandleFunc(eventsEndpoint, gateway.HandleEventsRoute(ctx, valkeyClient, hub, logger))

	req = httptest.NewRequest(http.MethodPost, eventsEndpoint, bytes.NewBuffer(alertPostBody2))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.JSONEq(t, post2Response, rec.Body.String())

	// one more time to emulate a scenario when alert was re-created or renamed
	mux = http.NewServeMux()
	mux.HandleFunc(eventsEndpoint, gateway.HandleEventsRoute(ctx, valkeyClient, hub, logger))

	req = httptest.NewRequest(http.MethodPost, eventsEndpoint, bytes.NewBuffer(alertPostBody3))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.JSONEq(t, post3Response, rec.Body.String())

	// TODO: Add tests for GET, POST partial MdaiEvent
}
