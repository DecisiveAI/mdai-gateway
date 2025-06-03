package prometheusalert

import (
	"sort"

	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/decisiveai/mdai-gateway/internal/jsonutil"
	"github.com/decisiveai/mdai-gateway/internal/utils"
	"go.uber.org/zap"
)

func (payload PrometheusAlertData) ToMdaiEvent(logger *zap.Logger) []eventing.MdaiEvent {
	sort.Slice(payload.Alerts, func(i, j int) bool {
		return payload.Alerts[i].StartsAt.Before(payload.Alerts[j].StartsAt)
	})

	mdaiEvents := make([]eventing.MdaiEvent, 0, len(payload.Alerts))

	for _, alert := range payload.Alerts {
		annotations := alert.Annotations
		labels := alert.Labels
		status := alert.Status

		unmarshalled := AlertPayload{
			Value:  annotations[CurrentValue],
			Status: status,
			Labels: labels,
		}

		payloadBytes, err := jsonutil.MarshalWithBuffer(unmarshalled)
		if err != nil {
			logger.Error("failed to marshal payload", zap.Error(err), zap.Object("payload", unmarshalled))
			return nil
		}

		id := alert.Fingerprint
		if id == "" {
			id = utils.CreateEventUUID()
		}

		mdaiEvent := eventing.MdaiEvent{
			Name:      annotations[AlertName] + "." + status,
			Source:    Prometheus,
			Id:        id,
			Timestamp: alert.StartsAt,
			HubName:   annotations[HubName],
			Payload:   string(payloadBytes),
		}

		logger.Debug("MdaiEvent created", zap.Object("event", mdaiEvent))
		mdaiEvents = append(mdaiEvents, mdaiEvent)
	}

	return mdaiEvents
}
