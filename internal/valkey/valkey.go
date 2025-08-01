package valkey

import (
	"context"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

func Init(ctx context.Context, logger *zap.Logger, cfg Config) valkey.Client { //nolint:ireturn
	retryCount := 0
	connectToValkey := func() (valkey.Client, error) {
		client, err := valkey.NewClient(valkey.ClientOption{
			InitAddress: cfg.InitAddress,
			Password:    cfg.Password,
		})
		if err != nil {
			retryCount++
			return nil, err
		}

		return client, nil
	}

	exponentialBackoff := backoff.NewExponentialBackOff()
	exponentialBackoff.InitialInterval = cfg.InitialBackoffInterval

	notifyFunc := func(err error, duration time.Duration) {
		logger.Warn("Failed to connect to Valkey. Retrying...",
			zap.Error(err),
			zap.Int("retry_count", retryCount),
			zap.Duration("duration", duration))
	}

	valkeyClient, err := backoff.Retry(
		ctx,
		connectToValkey,
		backoff.WithBackOff(exponentialBackoff),
		backoff.WithMaxElapsedTime(cfg.MaxBackoffElapsedTime),
		backoff.WithNotify(notifyFunc),
	)
	if err != nil {
		logger.Fatal("failed to get valkey client", zap.Error(err))
	}

	logger.Info("Connected to Valkey successfully", zap.Int("retry_count", retryCount))

	return valkeyClient
}
