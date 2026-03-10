package nats

import (
	"context"

	"github.com/mydecisive/mdai-data-core/eventing/publisher"
	"go.uber.org/zap"
)

func Init(ctx context.Context, logger *zap.Logger, clientName string) publisher.Publisher { //nolint:ireturn
	eventPublisher, err := publisher.NewPublisher(ctx, logger, clientName)
	if err != nil {
		logger.Fatal("initialising event publisher: %v", zap.Error(err))
	}
	return eventPublisher
}
