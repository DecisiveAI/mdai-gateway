package audit

import (
	"context"
	"strconv"
	"time"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"go.uber.org/zap"
)

type Inserter interface {
	InsertAuditLogEventFromMap(ctx context.Context, eventMap map[string]string) error
}

func RecordAuditEventFromMdaiEvent(ctx context.Context, logger *zap.Logger, auditAdapter Inserter, event eventing.MdaiEvent, success bool) error {
	eventMap := map[string]string{
		"id":              event.Id,
		"name":            event.Name,
		"timestamp":       event.Timestamp.UTC().Format(time.RFC3339),
		"payload":         event.Payload,
		"source":          event.Source,
		"sourceId":        event.SourceId,
		"correlation_id":  event.CorrelationId,
		"hub_name":        event.HubName,
		"publish_success": strconv.FormatBool(success),
	}
	logger.Info("AUDIT: Published event from Prometheus alert", zap.String("mdai-logstream", "audit"), zap.Any("mdaiEvent", eventMap))
	return auditAdapter.InsertAuditLogEventFromMap(ctx, eventMap)
}
