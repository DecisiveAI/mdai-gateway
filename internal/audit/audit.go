package audit

import (
	"context"
	"strconv"
	"time"

	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/decisiveai/mdai-event-hub/eventing"
	"go.uber.org/zap"
)

func RecordAuditEventFromMdaiEvent(ctx context.Context, logger *zap.Logger, auditAdapter *audit.AuditAdapter, event eventing.MdaiEvent, success bool) error {
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
