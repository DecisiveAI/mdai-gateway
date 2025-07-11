package main

import (
	"context"
	"net/http"

	"github.com/decisiveai/mdai-gateway/internal/server"
	"go.uber.org/zap"
)

func main() {
	ctx := context.Background()

	_, logger, teardown := initLogger()
	defer teardown()

	cfg := loadConfig(logger)

	deps, cleanup := initDependencies(ctx, cfg, logger)
	defer cleanup()

	router := server.NewRouter(ctx, deps)

	logger.Info("Starting server", zap.String("address", ":"+cfg.HTTPPort))

	httpServer := &http.Server{
		Addr:              ":" + cfg.HTTPPort,
		Handler:           router,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}

	logger.Fatal("failed to start server", zap.Error(httpServer.ListenAndServe()))
}
