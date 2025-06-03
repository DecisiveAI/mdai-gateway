package otel

import (
	"context"
	"errors"
	"fmt"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/zap"
)

type ZapErrorHandler struct {
	logger *zap.Logger
}

func (errorHandler ZapErrorHandler) Handle(err error) {
	if errorHandler.logger != nil {
		errorHandler.logger.Error(err.Error())
	}
}

type ShutdownFunc func(context.Context) error

// setupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func setupOTelSDK(ctx context.Context, internalLogger *zap.Logger) (ShutdownFunc, error) {
	var shutdownFuncs []ShutdownFunc

	otel.SetErrorHandler(&ZapErrorHandler{logger: internalLogger})

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown := func(ctx context.Context) error {
		var err error
		for _, fn := range shutdownFuncs {
			err = errors.Join(err, fn(ctx))
		}
		shutdownFuncs = nil
		return err
	}

	baseResource, err := resource.New(ctx,
		resource.WithAttributes(attribute.String("mdai-logstream", "hub")),
	)
	if err != nil {
		return shutdown, fmt.Errorf("failed to create OTEL resource: %w", err)
	}

	mergedResource, err := resource.Merge(resource.Default(), baseResource)
	if err != nil {
		return shutdown, fmt.Errorf("failed to merge OTEL resources: %w", err)
	}

	loggerProvider, err := newLoggerProvider(ctx, mergedResource)
	if err != nil {
		return shutdown, fmt.Errorf("failed to create logger provider: %w", err)
	}
	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	return shutdown, nil
}

func newLoggerProvider(ctx context.Context, res *resource.Resource) (*log.LoggerProvider, error) {
	logExporter, err := otlploghttp.New(ctx)
	if err != nil {
		return nil, err
	}

	loggerProvider := log.NewLoggerProvider(
		log.WithProcessor(log.NewBatchProcessor(logExporter)),
		log.WithResource(res),
	)
	return loggerProvider, nil
}
