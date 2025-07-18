package nats

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/decisiveai/mdai-event-hub/eventing"
	auditutils "github.com/decisiveai/mdai-gateway/internal/audit"
	"go.uber.org/zap"
)

func NewMdaiEvent(hubName string, varName string, varType string, action string, payload any) (*eventing.MdaiEvent, error) {
	payloadObj := eventing.ManualVariablesActionPayload{
		VariableRef: varName,
		DataType:    varType,
		Operation:   action,
		Data:        payload,
	}

	payloadBytes, err := json.Marshal(payloadObj)
	if err != nil {
		return nil, err
	}

	mdaiEvent := &eventing.MdaiEvent{
		Name:    strings.Join([]string{"manual_variable", action, hubName, varName}, "__"),
		HubName: hubName,
		Source:  eventing.ManualVariablesEventSource,
		Payload: string(payloadBytes),
	}
	mdaiEvent.ApplyDefaults()

	return mdaiEvent, nil
}

func PublishEvents(ctx context.Context, logger *zap.Logger, publisher eventing.Publisher, events []eventing.MdaiEvent, auditAdapter *audit.AuditAdapter) (int, error) {
	var (
		successCount int
		errs         []error
	)

	for _, event := range events {
		err := publishWithRetry(ctx, logger, publisher, event)

		if auditErr := auditutils.RecordAuditEventFromMdaiEvent(ctx, logger, auditAdapter, event, err == nil); auditErr != nil {
			logger.Error("Failed to write audit event for automation step",
				zap.String("hubName", event.HubName),
				zap.String("name", event.Name),
				zap.String("eventCorrelationId", event.CorrelationId),
				zap.String("publishSuccess", strconv.FormatBool(err == nil)),
				zap.Error(auditErr),
			)
		}

		if err == nil {
			successCount++
			continue
		}

		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			break
		}

		errs = append(errs, err)
	}

	return successCount, errors.Join(errs...)
}

func publishWithRetry(ctx context.Context, logger *zap.Logger, publisher eventing.Publisher, event eventing.MdaiEvent) error {
	const (
		maxRetries = 3
		retryDelay = 100 * time.Millisecond
	)

	var err error
	for attempt := 1; attempt <= maxRetries; attempt++ {
		if err = publisher.Publish(ctx, event); err == nil {
			return nil
		}

		if ctx.Err() != nil {
			return ctx.Err()
		}

		logger.Warn("Failed to publish event, will retry...",
			zap.String("event_name", event.Name),
			zap.Int("attempt", attempt),
			zap.Error(err),
		)

		select {
		case <-time.After(retryDelay):
		case <-ctx.Done():
			return ctx.Err()
		}
	}

	return fmt.Errorf("event %s failed after %d attempts: %w", event.Name, maxRetries, err)
}
