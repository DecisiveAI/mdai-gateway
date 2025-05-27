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

	"github.com/decisiveai/event-hub-poc/eventing"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"

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

func TestUpdateEventsHandler(t *testing.T) {
	const (
		post_1_response = `{"message":"Processed Prometheus alerts","successful":6,"total":6}
`
		post_2_response = `{"message":"Processed Prometheus alerts","successful":3,"total":3}
`
		post_3_response = `{"message":"Processed Prometheus alerts","successful":2,"total":2}
`
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
	hub, _ := eventing.NewEventHub("amqp://guest:guest@localhost:5672/", "test-queue", zap.NewExample()) // TODO mock the eventing.EventHub struct
	mux.HandleFunc("/events", handleEventsRoute(ctx, valkeyClient, hub))

	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBuffer(alertPostBody1))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, post_1_response, rec.Body.String())

	// one more time with different payload
	mux = http.NewServeMux()
	mux.HandleFunc("/events", handleEventsRoute(ctx, valkeyClient, hub))

	req = httptest.NewRequest(http.MethodPost, "/events", bytes.NewBuffer(alertPostBody2))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, post_2_response, rec.Body.String())

	// one more time to emulate a scenario when alert was re-created or renamed
	mux = http.NewServeMux()
	mux.HandleFunc("/events", handleEventsRoute(ctx, valkeyClient, hub))

	req = httptest.NewRequest(http.MethodPost, "/events", bytes.NewBuffer(alertPostBody3))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, post_3_response, rec.Body.String())
}
