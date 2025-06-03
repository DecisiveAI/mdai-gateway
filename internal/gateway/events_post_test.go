package gateway

import (
	"bytes"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"go.uber.org/zap"
)

func BenchmarkHandlePostRequest_PrometheusAlert(b *testing.B) {
	logger := zap.NewNop()
	hub := eventing.NewMockEventHub()

	body := []byte(`{
        "receiver": "webhook",
        "alerts": [{"status": "firing", "labels": {"alertname": "NoisyService"}}]
    }`)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ctx := b.Context()

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		w.Body.Reset()
		handlePostRequest(ctx, w, req, hub, logger)
	}
}

func BenchmarkHandlePostRequest_MdaiEvent(b *testing.B) {
	logger := zap.NewNop() // no-op logger for minimal impact
	hub := eventing.NewMockEventHub()

	body := []byte(`{
        "name": "TestEvent",
        "hubName": "mdai-hub",
        "payload": "some data"
    }`)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ctx := b.Context()

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		w.Body.Reset()
		handlePostRequest(ctx, w, req, hub, logger)
	}
}

func BenchmarkHandlePostRequest_Invalid(b *testing.B) {
	logger := zap.NewNop()
	hub := eventing.NewMockEventHub()

	body := []byte(`{"foo": "bar"}`)

	req := httptest.NewRequest(http.MethodPost, "/", bytes.NewReader(body))
	w := httptest.NewRecorder()

	ctx := b.Context()

	b.ResetTimer()
	b.ReportAllocs()
	for range b.N {
		w.Body.Reset()
		handlePostRequest(ctx, w, req, hub, logger)
	}
}
