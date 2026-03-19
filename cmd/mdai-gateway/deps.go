package main

import (
	"context"
	"crypto/tls"
	"fmt"
	"github.com/mydecisive/mdai-data-core/audit"
	datacorepublisher "github.com/mydecisive/mdai-data-core/eventing/publisher"
	datacorekube "github.com/mydecisive/mdai-data-core/kube"
	"github.com/mydecisive/mdai-data-core/service"
	"github.com/mydecisive/mdai-data-core/valkey"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	"github.com/mydecisive/mdai-gateway/internal/opamp"
	"github.com/mydecisive/mdai-gateway/internal/server"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
	"net/http"
	"os"
	"strings"
)

const (
	namespaceFilePath   = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	publisherClientName = "publisher-mdai-gateway"
)

func getCurrentNamespace() string {
	if ns := os.Getenv("POD_NAMESPACE"); ns != "" {
		return ns
	}

	if data, err := os.ReadFile(namespaceFilePath); err == nil {
		ns := strings.TrimSpace(string(data))
		if ns != "" {
			return ns
		}
	}

	return "default"
}

func initDependencies(ctx context.Context) (deps server.HandlerDeps, cleanup func()) { //nolint:nonamedreturns
	sysLogger, appLogger, teardownFn := service.InitLogger(ctx, serviceName)

	valkeyClient, err := valkey.Init(ctx, appLogger, valkey.NewConfig())
	if err != nil {
		appLogger.Fatal("failed to initialize valkey client", zap.Error(err))
	}

	auditAdapter := audit.NewAuditAdapter(appLogger, valkeyClient)

	publisher, err := datacorepublisher.NewPublisher(ctx, appLogger, publisherClientName)
	if err != nil {
		appLogger.Fatal("failed to start NATS publisher", zap.Error(err))
	}

	clientset, err := datacorekube.NewK8sClient(appLogger)
	if err != nil {
		appLogger.Fatal("failed to create Kubernetes client: %w", zap.Error(err))
	}

	cmController, err := startConfigMapController(appLogger, clientset, []string{datacorekube.ManualEnvConfigMapType}, corev1.NamespaceAll)
	if err != nil {
		appLogger.Fatal("failed to start config map controller", zap.Error(err))
	}

	deduper := adapter.NewDeduper()

	opampServer, err := opamp.NewOpAMPControlServer(appLogger, auditAdapter, publisher)
	if err != nil {
		appLogger.Fatal("failed to start OpAMP server", zap.Error(err))
	}

	httpTransport := &http.Transport{}
	if os.Getenv("TLS_INSECURE") == "true" {
		httpTransport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true}
	}

	httpClient := &http.Client{
		Transport: httpTransport,
	}

	deps = server.HandlerDeps{
		Logger:              appLogger,
		ValkeyClient:        valkeyClient,
		EventPublisher:      publisher,
		ConfigMapController: cmController,
		AuditAdapter:        auditAdapter,
		Deduper:             deduper,
		OpAMPServer:         opampServer,
		K8sClient:           clientset,
		K8sNamespace:        getCurrentNamespace(),
		HttpClient:          httpClient,
	}

	cleanup = func() {
		appLogger.Info("Closing client connections...")
		valkeyClient.Close()
		_ = publisher.Close()
		cmController.Stop()
		sysLogger.Info("Cleanup complete.")
		teardownFn()
	}

	return deps, cleanup
}

func startConfigMapController(
	logger *zap.Logger,
	clientset kubernetes.Interface,
	configMapTypes []string,
	namespace string,
) (*datacorekube.ConfigMapController, error) {
	controller, err := datacorekube.NewConfigMapController(configMapTypes, namespace, clientset, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create ConfigMap controller: %w", err)
	}

	if err := controller.Run(); err != nil {
		logger.Error("ConfigMap controller exited with error", zap.Error(err))
	}

	return controller, nil
}
