package main

import (
	"context"
	"net/http"

	"github.com/decisiveai/mdai-data-core/helpers"
	"github.com/decisiveai/mdai-gateway/internal/server"
	"go.uber.org/zap"
)

const serviceName = "github.com/decisiveai/mdai-gateway"

func main() {
	ctx := context.Background()

	deps, cleanup := initDependencies(ctx)
	defer cleanup()

	router := server.NewRouter(ctx, deps)

	httpPort := helpers.GetEnvVariableWithDefault(httpPortEnvVarKey, defaultHTTPPort)
	deps.Logger.Info("Starting server", zap.String("address", ":"+httpPort))

	httpServer := &http.Server{
		Addr:              ":" + httpPort,
		Handler:           router,
		ReadHeaderTimeout: defaultReadHeaderTimeout,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}

	deps.Logger.Fatal("failed to start server", zap.Error(httpServer.ListenAndServe()))
}
