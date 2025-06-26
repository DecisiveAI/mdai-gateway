package main

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"go.uber.org/zap"
	"k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/client-go/informers"
	coreinformers "k8s.io/client-go/informers/core/v1"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/rest"
	"k8s.io/client-go/tools/cache"
	"k8s.io/client-go/tools/clientcmd"
)

const (
	ByHub                      = "IndexByHub"
	ManagedByMdaiOperatorLabel = "app.kubernetes.io/managed-by=mdai-operator"
	envConfigMapType           = "hub-variables"
	manualEnvConfigMapType     = "hub-manual-variables"
	automationConfigMapType    = "hub-automation"
	LabelMdaiHubName           = "mydecisive.ai/hub-name"
	configMapTypeLabel         = "mydecisive.ai/configmap-type"
)

type ConfigMapController struct {
	informerFactory informers.SharedInformerFactory
	cmInformer      coreinformers.ConfigMapInformer
	lock            sync.RWMutex
	namespace       string
	configMapType   string
	logger          *log.Logger
}

func (c *ConfigMapController) Run(stopCh chan struct{}) error {
	c.informerFactory.Start(stopCh)

	if !cache.WaitForCacheSync(stopCh, c.cmInformer.Informer().HasSynced) {
		return fmt.Errorf("failed to sync")
	}
	return nil
}

func NewConfigMapController(configMapType string, namespace string) (*ConfigMapController, error) {

	defaultResyncTime := time.Hour * 24

	clientset, err := createK8sClient()
	if err != nil {
		logger.Fatal("failed to create k8s client", zap.Error(err))
	}

	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		defaultResyncTime,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			switch configMapType {
			case envConfigMapType, manualEnvConfigMapType, automationConfigMapType:
				{
					opts.LabelSelector = fmt.Sprintf("%s=%s", configMapTypeLabel, configMapType)
				}
			default:
				{
					logger.Error("Unsupported configMap, creating informer for all ConfigMaps, managed by Hub", zap.String("ConfigMap type", configMapType))
					opts.LabelSelector = ManagedByMdaiOperatorLabel
				}
			}
		}),
	)

	cmInformer := informerFactory.Core().V1().ConfigMaps()

	if err := cmInformer.Informer().AddIndexers(map[string]cache.IndexFunc{
		ByHub: func(obj interface{}) ([]string, error) {
			var hubNames []string
			hubName, err := getHubName(obj.(*v1.ConfigMap))
			if err != nil {
				logger.Error("failed to get hub name for ConfigMap", zap.String("ConfigMap name", obj.(*v1.ConfigMap).Name))
			}
			hubNames = append(hubNames, hubName)
			return hubNames, nil
		},
	}); err != nil {
		logger.Fatal("failed to add index", zap.Error(err))
		return nil, err
	}

	c := &ConfigMapController{
		namespace:       namespace,
		configMapType:   configMapType,
		informerFactory: informerFactory,
		cmInformer:      cmInformer,
		logger:          log.New(os.Stdout, "[ConfigMapManager] ", log.LstdFlags),
	}

	return c, nil
}

func getHubName(configMap *v1.ConfigMap) (string, error) {
	if configMap.Labels != nil && len(configMap.Labels) > 0 {
		hubName := configMap.Labels[LabelMdaiHubName]
		return hubName, nil
	}
	return "", fmt.Errorf("ConfigMap does not have hub name label")
}

func createK8sClient() (kubernetes.Interface, error) {
	config, err := rest.InClusterConfig()
	if err != nil {
		kubeconfig, err := os.UserHomeDir()
		if err != nil {
			logger.Fatal("Failed to load k8s config", zap.Error(err))
			return nil, err
		}

		config, err = clientcmd.BuildConfigFromFlags("", kubeconfig+"/.kube/config")
		if err != nil {
			logger.Fatal("Failed to build k8s config", zap.Error(err))
			return nil, err
		}
	}
	return kubernetes.NewForConfig(config)
}
