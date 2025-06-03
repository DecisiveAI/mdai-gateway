package main

import (
	"context"
	"errors"
	"net/http"

	"github.com/decisiveai/mdai-gateway/internal/gateway"
	"github.com/decisiveai/mdai-gateway/internal/otel"
	"github.com/decisiveai/mdai-gateway/internal/rmq"
	"github.com/decisiveai/mdai-gateway/internal/utils"
	"github.com/decisiveai/mdai-gateway/internal/valkey"
	"go.uber.org/zap"
)

func main() {
	internalLogger, logger, cleanup := initLogger()
	defer cleanup()

	ctx := context.Background()

	if err := otel.Setup(ctx, logger, internalLogger); err != nil {
		logger.Warn("Failed to setup OTEL", zap.Error(err))
	}

	valkeyClient, err := valkey.Init(ctx, logger)
	if err != nil {
		logger.Fatal("failed to initialize valkey", zap.Error(err))
	}
	defer valkeyClient.Close()

	hub, err := rmq.Init(ctx, logger)
	if err != nil {
		logger.Fatal("failed to initialize rmq", zap.Error(err))
	}
	defer hub.Close()

	http.HandleFunc(eventsEndpoint, gateway.HandleEventsRoute(ctx, valkeyClient, hub, logger))

	httpPort := utils.GetEnvVariableWithDefault(httpPortEnvVarKey, defaultHTTPPort)
	srv := &http.Server{
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		Addr:              ":" + httpPort,
		Handler:           nil, // or your custom ServeMux
	}

	go func() {
		logger.Info("Starting server", zap.String("address", srv.Addr))
		if err := srv.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			logger.Fatal("server failed", zap.Error(err))
		}
	}()

	<-ctx.Done()
	ctxTimeout, cancel := context.WithTimeout(context.Background(), contextCancelTimeout)
	defer cancel()
	if err := srv.Shutdown(ctxTimeout); err != nil {
		logger.Error("server shutdown failed", zap.Error(err))
	}
}
