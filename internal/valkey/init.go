package valkey

import (
	"context"
	"fmt"
	"time"

	"github.com/cenkalti/backoff/v5"
	"github.com/decisiveai/mdai-gateway/internal/utils"
	"github.com/valkey-io/valkey-go"
	"go.uber.org/zap"
)

const (
	maxRetryBackoff        = 3 * time.Minute
	initialBackoffInterval = 5 * time.Second
)

func Init(ctx context.Context, logger *zap.Logger) (valkey.Client, error) { //nolint:ireturn
	var retryCount int
	connectToValkey := func() (valkey.Client, error) {
		client, err := valkey.NewClient(valkey.ClientOption{
			InitAddress: []string{utils.GetEnvVariableWithDefault(valkeyEndpointEnvVarKey, "mdai-valkey-primary.mdai.svc.cluster.local:6379")},
			Password:    utils.GetEnvVariableWithDefault(valkeyPasswordEnvVarKey, "abc"),
		})
		if err != nil {
			retryCount++
			return nil, err
		}
		return client, nil
	}

	notify := func(err error, next time.Duration) {
		logger.Warn("failed to initialize valkey. retrying...",
			zap.Error(err),
			zap.Int("retry_count", retryCount),
			zap.Duration("next_attempt_in", next))
	}

	exponentialBackoff := backoff.NewExponentialBackOff()
	exponentialBackoff.InitialInterval = initialBackoffInterval

	valkeyClient, err := backoff.Retry(
		ctx,
		connectToValkey,
		backoff.WithBackOff(exponentialBackoff),
		backoff.WithMaxElapsedTime(maxRetryBackoff),
		backoff.WithNotify(notify),
	)
	if err != nil {
		return nil, fmt.Errorf("failed to get valkey client: %w", err)
	}

	return valkeyClient, nil
}
