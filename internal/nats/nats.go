package nats

import (
	"context"

	"github.com/decisiveai/mdai-event-hub/pkg/eventing"
	"github.com/decisiveai/mdai-event-hub/pkg/eventing/nats"
	"go.uber.org/zap"
)

func Init(_ context.Context, logger *zap.Logger, clientName string) eventing.Publisher { //nolint:ireturn
	publisher, err := nats.NewPublisher(logger, clientName)
	if err != nil {
		logger.Fatal("initialising event publisher: %v", zap.Error(err))
	}
	return publisher
}
