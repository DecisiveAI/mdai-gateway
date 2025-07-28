package nats

import (
	"context"
	"encoding/json"
	"errors"
	"strconv"
	"strings"

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
		err := publisher.Publish(ctx, event)

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
			errs = append(errs, err)
			break
		}

		errs = append(errs, err)
	}

	return successCount, errors.Join(errs...)
}
