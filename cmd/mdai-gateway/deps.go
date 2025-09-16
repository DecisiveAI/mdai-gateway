package main

import (
	"context"
	"fmt"
	"github.com/decisiveai/mdai-data-core/audit"
	datacorepublisher "github.com/decisiveai/mdai-data-core/eventing/publisher"
	datacorekube "github.com/decisiveai/mdai-data-core/kube"
	"github.com/decisiveai/mdai-data-core/service"
	"github.com/decisiveai/mdai-data-core/valkey"
	"github.com/decisiveai/mdai-gateway/internal/adapter"
	"github.com/decisiveai/mdai-gateway/internal/opamp"
	"github.com/decisiveai/mdai-gateway/internal/server"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const publisherClientName = "publisher-mdai-gateway"

func initDependencies(ctx context.Context) (deps server.HandlerDeps, cleanup func()) { //nolint:nonamedreturns
	sys, app, teardown := service.InitLogger(ctx, serviceName)

	valkeyClient, err := valkey.Init(ctx, app, valkey.NewConfig())
	if err != nil {
		app.Fatal("failed to initialize valkey client", zap.Error(err))
	}

	auditAdapter := audit.NewAuditAdapter(app, valkeyClient)

	publisher := nats.Init(ctx, app, publisherClientName)

	cmController, err := startConfigMapControllerWithClient(app, datacorekube.ManualEnvConfigMapType, corev1.NamespaceAll)
	if err != nil {
		app.Fatal("failed to initialize valkey client", zap.Error(err))
	}

	auditAdapter := audit.NewAuditAdapter(app, valkeyClient)

	publisher, err := datacorepublisher.NewPublisher(ctx, app, publisherClientName)
	if err != nil {
		app.Fatal("failed to start NATS publisher", zap.Error(err))
	}

	cmController, err := startConfigMapControllerWithClient(app, datacorekube.ManualEnvConfigMapType, corev1.NamespaceAll)
	if err != nil {
		app.Fatal("failed to start config map controller", zap.Error(err))
	}

	deduper := adapter.NewDeduper()

	opampServer := opamp.NewOpAMPControlServer(app, auditAdapter, publisher)
	opampHandler, connCtx, err := opampServer.GetOpAMPHTTPHandler()
	if err != nil {
		app.Fatal("failed to start OpAMP server", zap.Error(err))
	}

	deps = server.HandlerDeps{
		Logger:              app,
		ValkeyClient:        valkeyClient,
		EventPublisher:      publisher,
		ConfigMapController: cmController,
		AuditAdapter:        auditAdapter,
		Deduper:             deduper,
		OpAMPHandler:        opampHandler,
		OpAMPConnCtx:        connCtx,
	}

	cleanup = func() {
		app.Info("Closing client connections...")
		valkeyClient.Close()
		_ = publisher.Close()
		cmController.Stop()
		sys.Info("Cleanup complete.")
		teardown()
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
