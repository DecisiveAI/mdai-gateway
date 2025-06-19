package main

import (
	"context"
	"fmt"

	datacorekube "github.com/decisiveai/mdai-data-core/kube"
	"github.com/decisiveai/mdai-gateway/internal/nats"
	"github.com/decisiveai/mdai-gateway/internal/server"
	"github.com/decisiveai/mdai-gateway/internal/valkey"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const publisherClientName = "publisher-mdai-gateway"

func initDependencies(ctx context.Context, cfg *Config, logger *zap.Logger) (deps server.HandlerDeps, cleanup func()) { //nolint:nonamedreturns
	var (
		otelShutdown shutdownFunc
		err          error
	)

	if !cfg.OTelDisabled {
		otelShutdown, err = setupOTelSDK(ctx, logger)
		if err != nil {
			logger.Fatal("Error setting up OpenTelemetry SDK", zap.Error(err))
		}
	}

	valkeyClient := valkey.Init(ctx, logger, cfg.ValkeyCfg)
	publisher := nats.Init(ctx, logger, publisherClientName)

	cmController, stopConfigMapController, err := startConfigMapControllerWithClient(logger, datacorekube.ManualEnvConfigMapType, corev1.NamespaceAll)
	if err != nil {
		logger.Fatal("failed to start config map controller", zap.Error(err))
	}

	deps = server.HandlerDeps{
		Logger:              logger,
		ValkeyClient:        valkeyClient,
		EventPublisher:      publisher,
		ConfigMapController: cmController,
	}

	cleanup = func() {
		logger.Info("Closing client connections...")
		valkeyClient.Close()
		_ = publisher.Close()
		if otelShutdown != nil {
			if err := otelShutdown(ctx); err != nil {
				logger.Error("OTEL SDK did not shut down gracefully!", zap.Error(err))
			}
		}
		if stopConfigMapController != nil {
			stopConfigMapController()
		}
		logger.Info("Cleanup complete.")
	}

	return deps, cleanup
}

func startConfigMapController(
	logger *zap.Logger,
	clientset kubernetes.Interface,
	configMapType string,
	namespace string,
) (*datacorekube.ConfigMapController, func(), error) {
	controller, err := datacorekube.NewConfigMapController(configMapType, namespace, clientset, logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create ConfigMap controller: %w", err)
	}

	stop := make(chan struct{})

	go func() {
		if err := controller.Run(stop); err != nil {
			logger.Error("ConfigMap controller exited with error", zap.Error(err))
		}
	}()

	stopFunc := func() {
		select {
		case <-stop:
			// already closed, do nothing
		default:
			close(stop)
		}
	}

	return controller, stopFunc, nil
}

func startConfigMapControllerWithClient(
	logger *zap.Logger,
	configMapType string,
	namespace string,
) (*datacorekube.ConfigMapController, func(), error) {
	clientset, err := datacorekube.NewK8sClient(logger)
	if err != nil {
		return nil, nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return startConfigMapController(logger, clientset, configMapType, namespace)
}
