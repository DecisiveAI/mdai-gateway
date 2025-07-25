package main

import (
	"context"
	"errors"
	"os"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlplog/otlploghttp"
	"go.opentelemetry.io/otel/log/global"
	"go.opentelemetry.io/otel/sdk/log"
	"go.opentelemetry.io/otel/sdk/resource"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

func initLogger() (internalLogger *zap.Logger, logger *zap.Logger, cleanup func()) { //nolint:nonamedreturns
	// Define custom encoder configuration
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"                   // Rename the time field
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder // Use human-readable timestamps
	encoderConfig.CallerKey = "caller"                    // Show caller file and line number

	core := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig), // JSON logging with readable timestamps
		zapcore.Lock(os.Stdout),               // Output to stdout
		zap.DebugLevel,                        // Log info and above
	)

	internalLogger = zap.New(core, zap.AddCaller())

	otelCore := otelzap.NewCore("github.com/decisiveai/mdai-gateway")
	multiCore := zapcore.NewTee(core, otelCore)
	logger = zap.New(multiCore, zap.AddCaller())

	cleanup = func() { _ = internalLogger.Sync(); _ = logger.Sync() }

	return internalLogger, logger, cleanup
}

type ZapErrorHandler struct {
	logger *zap.Logger
}

func (errorHandler ZapErrorHandler) Handle(err error) {
	if errorHandler.logger != nil {
		errorHandler.logger.Error(err.Error())
	}
}

type shutdownFunc func(context.Context) error

// setupOTelSDK bootstraps the OpenTelemetry pipeline.
// If it does not return an error, make sure to call shutdown for proper cleanup.
func setupOTelSDK(ctx context.Context, internalLogger *zap.Logger) (shutdown shutdownFunc, err error) { //nolint:nonamedreturns
	var shutdownFuncs []shutdownFunc

	otel.SetErrorHandler(&ZapErrorHandler{logger: internalLogger})

	// shutdown calls cleanup functions registered via shutdownFuncs.
	// The errors from the calls are joined.
	// Each registered cleanup will be invoked once.
	shutdown = func(ctx context.Context) error {
		var errs []error

		for _, fn := range shutdownFuncs {
			fnErr := fn(ctx)
			if fnErr != nil {
				errs = append(errs, fnErr)
			}
		}
		shutdownFuncs = nil
		return errors.Join(errs...)
	}

	// handleErr calls shutdown for cleanup and makes sure that all errors are returned.
	handleErr := func(inErr error) {
		err = errors.Join(inErr, shutdown(ctx))
	}

	resourceWAttributes, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("mdai-logstream", "hub"),
	))
	if err != nil {
		panic(err)
	}

	eventHandlerResource, err := resource.Merge(
		resource.Default(),
		resourceWAttributes,
	)
	if err != nil {
		panic(err)
	}

	// Set up logger provider.
	loggerProvider, err := newLoggerProvider(ctx, eventHandlerResource)
	if err != nil {
		handleErr(err)
		return shutdown, err
	}

	shutdownFuncs = append(shutdownFuncs, loggerProvider.Shutdown)
	global.SetLoggerProvider(loggerProvider)

	return shutdown, err
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
