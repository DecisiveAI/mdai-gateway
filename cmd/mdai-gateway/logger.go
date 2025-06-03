package main

import (
	"os"

	"go.opentelemetry.io/contrib/bridges/otelzap"
	"go.uber.org/zap"
	"go.uber.org/zap/zapcore"
)

type cleanupFunc func()

func initLogger() (internalLogger *zap.Logger, logger *zap.Logger, cleanup cleanupFunc) { //nolint:nonamedreturns
	// Define custom encoder configuration
	encoderConfig := zap.NewProductionEncoderConfig()
	encoderConfig.TimeKey = "timestamp"                   // Rename the time field
	encoderConfig.EncodeTime = zapcore.ISO8601TimeEncoder // Use human-readable timestamps
	encoderConfig.CallerKey = "caller"                    // Show caller file and line number

	baseCore := zapcore.NewCore(
		zapcore.NewJSONEncoder(encoderConfig), // JSON logging with readable timestamps
		zapcore.Lock(os.Stdout),               // Output to stdout
		zap.DebugLevel,                        // Log info and above
	)
	otelCore := otelzap.NewCore(otelZapCoreName)
	multiCore := zapcore.NewTee(baseCore, otelCore)

	internalLogger = zap.New(baseCore, zap.AddCaller())
	logger = zap.New(multiCore, zap.AddCaller())
	cleanup = func() { _ = internalLogger.Sync(); _ = logger.Sync() }

	return internalLogger, logger, cleanup
}
