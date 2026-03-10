package nats

import (
	"context"
	"errors"
	"strconv"

	"github.com/mydecisive/mdai-data-core/audit"
	"github.com/mydecisive/mdai-data-core/eventing/publisher"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	auditutils "github.com/mydecisive/mdai-gateway/internal/audit"
	"go.uber.org/zap"
)

func PublishEvents(ctx context.Context, logger *zap.Logger, p publisher.Publisher, eventsPerSubjects []adapter.EventPerSubject, auditAdapter *audit.AuditAdapter) (int, error) {
	var (
		successCount int
		errs         []error
	)

	for _, eventPerSubject := range eventsPerSubjects {
		event := eventPerSubject.Event
		err := p.Publish(ctx, event, eventPerSubject.Subject)

		if auditErr := auditutils.RecordAuditEventFromMdaiEvent(ctx, logger, auditAdapter, event, err == nil); auditErr != nil {
			logger.Error("Failed to write audit event for automation step",
				zap.String("hubName", event.HubName),
				zap.String("name", event.Name),
				zap.String("eventCorrelationId", event.CorrelationID),
				zap.String("publishSuccess", strconv.FormatBool(err == nil)),
				zap.Error(auditErr),
			)
		}

		if err == nil {
			successCount++
			continue
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			errs = append(errs, err)
			break
		}

		errs = append(errs, err)
	}

	return successCount, errors.Join(errs...)
}
