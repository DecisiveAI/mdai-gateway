package main

import (
	"context"
	"fmt"

	"github.com/decisiveai/mdai-data-core/audit"
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

	auditAdapter := audit.NewAuditAdapter(logger, valkeyClient, cfg.ValkeyCfg.AuditStreamExpiration)

	publisher := nats.Init(ctx, logger, publisherClientName)

	cmController, err := startConfigMapControllerWithClient(logger, datacorekube.ManualEnvConfigMapType, corev1.NamespaceAll)
	if err != nil {
		logger.Fatal("failed to start config map controller", zap.Error(err))
	}

	deps = server.HandlerDeps{
		Logger:              logger,
		ValkeyClient:        valkeyClient,
		EventPublisher:      publisher,
		ConfigMapController: cmController,
		AuditAdapter:        auditAdapter,
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
		cmController.Stop()
		logger.Info("Cleanup complete.")
	}

	return deps, cleanup
}

func startConfigMapController(
	logger *zap.Logger,
	clientset kubernetes.Interface,
	configMapType string,
	namespace string,
) (*datacorekube.ConfigMapController, error) {
	controller, err := datacorekube.NewConfigMapController(configMapType, namespace, clientset, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create ConfigMap controller: %w", err)
	}

	if err := controller.Run(); err != nil {
		logger.Error("ConfigMap controller exited with error", zap.Error(err))
	}

	return controller, nil
}

func startConfigMapControllerWithClient(
	logger *zap.Logger,
	configMapType string,
	namespace string,
) (*datacorekube.ConfigMapController, error) {
	clientset, err := datacorekube.NewK8sClient(logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create Kubernetes client: %w", err)
	}

	return startConfigMapController(logger, clientset, configMapType, namespace)
}
