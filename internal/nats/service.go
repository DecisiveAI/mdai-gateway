package nats

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"

	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/decisiveai/mdai-event-hub/pkg/eventing"
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
		Name:    "var" + "." + action,
		HubName: hubName,
		Source:  eventing.ManualVariablesEventSource,
		Payload: string(payloadBytes),
	}
	mdaiEvent.ApplyDefaults()

	return mdaiEvent, nil
}

func PublishEvents(ctx context.Context, logger *zap.Logger, publisher eventing.Publisher, eventsPerSubjects []eventing.EventPerSubject, auditAdapter *audit.AuditAdapter) (int, error) {
	var (
		successCount int
		errs         []error
	)

	for _, eventPerSubject := range eventsPerSubjects {
		event := eventPerSubject.Event
		err := publisher.Publish(ctx, event, eventPerSubject.Subject)

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
