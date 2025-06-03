package rmq

import (
	"context"
	"fmt"
	"net/url"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/decisiveai/mdai-event-hub/eventing"
	"github.com/decisiveai/mdai-gateway/internal/utils"
	"go.uber.org/zap"
)

const (
	maxRetryBackoff        = 3 * time.Minute
	initialBackoffInterval = 5 * time.Second
)

func Init(ctx context.Context, logger *zap.Logger) (eventing.EventHubInterface, error) { //nolint:ireturn
	var retryCount int
	connectToRmq := func() (eventing.EventHubInterface, error) {
		password := utils.GetEnvVariableWithDefault(rabbitmqPasswordEnvVarKey, "")
		endpoint := utils.GetEnvVariableWithDefault(rabbitmqEndpointEnvVarKey, "localhost:5672")

		hubURL := &url.URL{
			Scheme: "amqp",
			Host:   endpoint,
			User:   url.UserPassword("mdai", password),
		}

		hub, err := eventing.NewEventHub(hubURL.String(), eventing.EventQueueName, logger)
		if err != nil {
			retryCount++
			return nil, err
		}
		logger.Info("Successfully created EventHub", zap.String("endpoint", endpoint))
		return hub, nil
	}

	notify := func(err error, next time.Duration) {
		logger.Warn("failed to initialize rmq. retrying...",
			zap.Error(err),
			zap.Int("retry_count", retryCount),
			zap.Duration("next_attempt_in", next))
	}

	exponentialBackoff := backoff.NewExponentialBackOff()
	exponentialBackoff.InitialInterval = initialBackoffInterval

	hub, err := backoff.Retry(
		ctx,
		connectToRmq,
		backoff.WithBackOff(exponentialBackoff),
		backoff.WithMaxElapsedTime(maxRetryBackoff),
		backoff.WithNotify(notify),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to connect to rmq: %w", err)
	}
	return hub, nil
}
