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
	"time"

	"github.com/decisiveai/mdai-event-hub/eventing/nats"
	"github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"github.com/valkey-io/valkey-go"
	"github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
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

	srv, _ := runJetStream(t)
	defer srv.Shutdown()

	logger, err := zap.NewDevelopment()
	assert.NoError(t, err)

	publisher, err := nats.NewPublisher(logger, "publisher-mdai-gateway")
	assert.NoError(t, err)
	defer func(publisher *nats.EventPublisher) {
		_ = publisher.Close()
	}(publisher)

	mux.HandleFunc("/events", handleEventsRoute(ctx, valkeyClient, publisher))

	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewBuffer(alertPostBody1))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, post_1_response, rec.Body.String())

	// one more time with different payload
	mux = http.NewServeMux()
	mux.HandleFunc("/events", handleEventsRoute(ctx, valkeyClient, publisher))

	req = httptest.NewRequest(http.MethodPost, "/events", bytes.NewBuffer(alertPostBody2))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, post_2_response, rec.Body.String())

	// one more time to emulate a scenario when alert was re-created or renamed
	mux = http.NewServeMux()
	mux.HandleFunc("/events", handleEventsRoute(ctx, valkeyClient, publisher))

	req = httptest.NewRequest(http.MethodPost, "/events", bytes.NewBuffer(alertPostBody3))
	req.Header.Set("Content-Type", "application/json")
	rec = httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusCreated, rec.Code)
	require.Equal(t, post_3_response, rec.Body.String())

	// TODO: Add tests for GET, POST partial MdaiEvent
}

func TestMdaiEventPostHandler_NatsPublishFails(t *testing.T) {
	ctx := context.TODO()
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	valkeyClient := mock.NewClient(ctrl)
	srv, _ := runJetStream(t)
	defer srv.Shutdown()

	logger, err := zap.NewDevelopment()
	require.NoError(t, err)

	publisher, err := nats.NewPublisher(logger, "publisher-mdai-gateway")
	assert.NoError(t, err)
	_ = publisher.Close() // close the publisher to simulate failure

	mux := http.NewServeMux()
	mux.HandleFunc("/events", handleEventsRoute(ctx, valkeyClient, publisher))

	mdaiBody, err := os.ReadFile("testdata/event-part1.json")
	require.NoError(t, err)

	req := httptest.NewRequest(http.MethodPost, "/events", bytes.NewReader(mdaiBody))
	req.Header.Set("Content-Type", "application/json")
	rec := httptest.NewRecorder()
	mux.ServeHTTP(rec, req)

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	assert.Contains(t, rec.Body.String(), "Failed to publish event")
}

func runJetStream(t *testing.T) (*server.Server, string) {
	t.Helper()
	tempDir := t.TempDir()
	ns, err := server.NewServer(&server.Options{
		JetStream: true,
		StoreDir:  tempDir,
		Port:      -1, // pick a random free port
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats-server did not start")
	}
	assert.NoError(t, os.Setenv("NATS_URL", ns.ClientURL()))

	return ns, ns.ClientURL()
}
