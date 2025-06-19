package k8s

import (
	mdaiv1 "github.com/decisiveai/mdai-operator/api/v1"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	"k8s.io/apimachinery/pkg/runtime"
	utilruntime "k8s.io/apimachinery/pkg/util/runtime"
	"sigs.k8s.io/controller-runtime/pkg/client"
	"sigs.k8s.io/controller-runtime/pkg/client/config"
)

func Init(logger *zap.Logger) (client.Client, error) { //nolint:ireturn
	cfg, err := config.GetConfig()
	if err != nil {
		logger.Error("Failed to get Kubernetes config", zap.Error(err))
		return nil, err
	}

	scheme := runtime.NewScheme()
	utilruntime.Must(corev1.AddToScheme(scheme))
	utilruntime.Must(mdaiv1.AddToScheme(scheme))

	cl, err := client.New(cfg, client.Options{Scheme: scheme})
	if err != nil {
		logger.Error("Failed to create controller-runtime client", zap.Error(err))
		return nil, err
	}

	logger.Info("Successfully initialized controller-runtime client")

	return cl, nil
}
