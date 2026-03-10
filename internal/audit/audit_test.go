package audit

import (
	"testing"
	"time"

	"github.com/mydecisive/mdai-data-core/eventing"
	"github.com/mydecisive/mdai-gateway/mocks"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"go.uber.org/zap"
	"go.uber.org/zap/zaptest/observer"
)

func TestRecordAuditEventFromMdaiEvent(t *testing.T) {
	core, obs := observer.New(zap.InfoLevel)
	logger := zap.New(core)
	t.Cleanup(func() { _ = logger.Sync() })

	mockAudit := &mocks.MockAuditAdapter{}
	event := eventing.MdaiEvent{
		ID:            "id1",
		Name:          "event_name",
		Timestamp:     time.Date(2025, 7, 19, 12, 0, 0, 0, time.UTC),
		Payload:       "{}",
		Source:        "source",
		SourceID:      "src1",
		CorrelationID: "cid1",
		HubName:       "hub",
	}

	expectedMap := map[string]string{
		"id":              "id1",
		"name":            "event_name",
		"timestamp":       "2025-07-19T12:00:00Z",
		"payload":         "{}",
		"source":          "source",
		"sourceId":        "src1",
		"correlation_id":  "cid1",
		"hub_name":        "hub",
		"publish_success": "true",
	}

	mockAudit.On("InsertAuditLogEventFromMap", t.Context(), expectedMap).Return(nil).Once()

	err := RecordAuditEventFromMdaiEvent(t.Context(), logger, mockAudit, event, true)
	require.NoError(t, err)

	mockAudit.AssertExpectations(t)

	logs := obs.All()
	require.Len(t, logs, 1)
	log := logs[0]

	assert.Equal(t, "AUDIT: Published event from Prometheus alert", log.Message)
	assert.Equal(t, "audit", log.ContextMap()["mdai-logstream"])

	eventMap, ok := log.ContextMap()["mdaiEvent"].(map[string]string)
	require.True(t, ok)

	assert.Equal(t, "id1", eventMap["id"])
	assert.Equal(t, "event_name", eventMap["name"])
	assert.Equal(t, "true", eventMap["publish_success"])
}
