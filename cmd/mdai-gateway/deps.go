package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/mydecisive/mdai-data-core/audit"
	datacorepublisher "github.com/mydecisive/mdai-data-core/eventing/publisher"
	datacorekube "github.com/mydecisive/mdai-data-core/kube"
	"github.com/mydecisive/mdai-data-core/service"
	datacorevalkey "github.com/mydecisive/mdai-data-core/valkey"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	"github.com/mydecisive/mdai-gateway/internal/opamp"
	"github.com/mydecisive/mdai-gateway/internal/server"
	gatewayvalkey "github.com/mydecisive/mdai-gateway/internal/valkey"
	"github.com/prometheus/client_golang/api"
	promv1 "github.com/prometheus/client_golang/api/prometheus/v1"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/client-go/kubernetes"
)

const (
	namespaceFilePath      = "/var/run/secrets/kubernetes.io/serviceaccount/namespace"
	publisherClientName    = "publisher-mdai-gateway"
	slowValueReadThreshold = 250 * time.Millisecond
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

	valkeyClient, err := datacorevalkey.Init(ctx, appLogger, datacorevalkey.NewConfig())
	if err != nil {
		appLogger.Fatal("failed to initialize valkey client", zap.Error(err))
	}
	variableReader := gatewayvalkey.NewReader(valkeyClient, appLogger)

	auditAdapter := audit.NewAuditAdapter(appLogger, valkeyClient)

	publisher, err := datacorepublisher.NewPublisher(ctx, appLogger, publisherClientName)
	if err != nil {
		appLogger.Fatal("failed to start NATS publisher", zap.Error(err))
	}

	clientset, err := datacorekube.NewK8sClient(appLogger)
	if err != nil {
		appLogger.Fatal("failed to create Kubernetes client: %w", zap.Error(err))
	}

	cmController, err := startConfigMapController(appLogger, clientset,
		[]string{
			datacorekube.VariablesSchemaMapType,
		},
		corev1.NamespaceAll,
	)
	if err != nil {
		appLogger.Fatal("failed to start config map controller", zap.Error(err))
	}

	deduper := adapter.NewDeduper()

	opampServer, err := opamp.NewOpAMPControlServer(appLogger, auditAdapter, publisher)
	if err != nil {
		appLogger.Fatal("failed to start OpAMP server", zap.Error(err))
	}

	promEndpoint := os.Getenv("PROMETHEUS_ENDPOINT")
	if promEndpoint == "" {
		appLogger.Warn("prometheus endpoint is not set, octant connections API will not function properly")
	}

	client, err := api.NewClient(api.Config{
		Address: promEndpoint,
	})
	if err != nil {
		appLogger.Fatal("failed to create prometheus client", zap.Error(err))
	}

	deps = server.HandlerDeps{
		Logger:                 appLogger,
		ValkeyClient:           valkeyClient,
		VariableReader:         variableReader,
		SlowValueReadThreshold: slowValueReadThreshold,
		EventPublisher:         publisher,
		ConfigMapController:    cmController,
		AuditAdapter:           auditAdapter,
		Deduper:                deduper,
		OpAMPServer:            opampServer,
		K8sClient:              clientset,
		K8sNamespace:           getCurrentNamespace(),
		PrometheusClient:       promv1.NewAPI(client),
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
		return nil, fmt.Errorf("failed to sync configmap controller cache: %w", err)
	}

	return controller, nil
}
