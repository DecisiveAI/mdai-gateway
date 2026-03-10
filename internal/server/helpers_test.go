package server

import (
	"encoding/json"
	"errors"
	"fmt"
	"testing"
	"time"

	"github.com/mydecisive/mdai-data-core/audit"
	"github.com/mydecisive/mdai-data-core/eventing/publisher"
	datacorekube "github.com/mydecisive/mdai-data-core/kube"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	"github.com/mydecisive/mdai-gateway/internal/opamp"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/stretchr/testify/require"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/informers"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
	"k8s.io/client-go/tools/cache"
)

func newFakeClientset(t *testing.T) kubernetes.Interface { //nolint:ireturn
	t.Helper()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	configMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mdaihub-sample-manual-variables",
			Namespace: "mdai",
			Labels: map[string]string{
				datacorekube.ConfigMapTypeLabel: datacorekube.ManualEnvConfigMapType,
				datacorekube.LabelMdaiHubName:   "mdaihub-sample",
			},
		},
		Data: map[string]string{
			"data_boolean": "boolean",
			"data_map":     "map",
			"data_set":     "set",
			"data_string":  "string",
			"data_int":     "int",
		},
	}

	return fake.NewClientset(&configMap)
}

func newFakeConfigMapController(t *testing.T, clientset kubernetes.Interface, namespace string) (*datacorekube.ConfigMapController, error) {
	t.Helper()
	defaultResyncTime := 0 * time.Second

	informerFactory := informers.NewSharedInformerFactoryWithOptions(
		clientset,
		defaultResyncTime,
		informers.WithNamespace(namespace),
		informers.WithTweakListOptions(func(opts *metav1.ListOptions) {
			opts.LabelSelector = fmt.Sprintf("%s=%s", datacorekube.ConfigMapTypeLabel, datacorekube.ManualEnvConfigMapType)
		},
		),
	)
	cmInformer := informerFactory.Core().V1().ConfigMaps()

	if err := cmInformer.Informer().AddIndexers(map[string]cache.IndexFunc{
		datacorekube.ByHub: func(obj any) ([]string, error) {
			var hubNames []string
			hubName := "mdaihub-sample"
			hubNames = append(hubNames, hubName)
			return hubNames, nil
		},
	}); err != nil {
		t.Fatal("failed to add index", err)
		return nil, err
	}

	c, err := datacorekube.NewConfigMapController([]string{datacorekube.ManualEnvConfigMapType}, namespace, clientset, zap.NewNop())
	if err != nil {
		return nil, err
	}

	if err := c.Run(); err != nil {
		t.Errorf("Controller failed to run: %v", err)
	}

	stopCh := make(chan struct{})
	if !cache.WaitForCacheSync(stopCh, c.CmInformer.Informer().HasSynced) {
		return nil, errors.New("failed to sync informer caches")
	}

	return c, nil
}

type errReader struct{}

func (*errReader) Read(_ []byte) (int, error) {
	return 0, errors.New("forced read error")
}

func (*errReader) Close() error {
	return nil
}

func ptr[T any](v T) *T {
	return &v
}

func stringifyData(t *testing.T, body string) string {
	t.Helper()

	var parsed struct {
		Data any `json:"data"`
	}
	if err := json.Unmarshal([]byte(body), &parsed); err != nil {
		t.Fatalf("invalid JSON body: %v", err)
	}

	switch parsedValue := parsed.Data.(type) {
	case string, float64, bool:
		return fmt.Sprintf("%q", fmt.Sprintf("%v", parsedValue))
	default:
		b, err := json.Marshal(parsed.Data)
		if err != nil {
			t.Fatalf("failed to marshal structured data: %v", err)
		}

		return string(b)
	}
}

func runJetStream(t *testing.T) *natsserver.Server {
	t.Helper()
	tempDir := t.TempDir()
	ns, err := natsserver.NewServer(&natsserver.Options{
		JetStream: true,
		StoreDir:  tempDir,
		Port:      -1, // pick a random free port
	})
	if err != nil {
		t.Fatalf("new server: %v", err)
	}
	go ns.Start()
	if !ns.ReadyForConnections(5 * time.Second) {
		t.Fatal("nats-server did not start")
	}
	t.Setenv("NATS_URL", ns.ClientURL())
	return ns
}

func setupMocks(t *testing.T, clientset kubernetes.Interface) HandlerDeps {
	t.Helper()

	ctrl := gomock.NewController(t)
	valkeyClient := valkeymock.NewClient(ctrl)
	auditAdapter := audit.NewAuditAdapter(zap.NewNop(), valkeyClient)

	srv := runJetStream(t)
	t.Cleanup(func() { srv.Shutdown() })
	eventPublisher, err := publisher.NewPublisher(t.Context(), zap.NewNop(), publisherClientName)
	require.NoError(t, err)
	t.Cleanup(func() { _ = eventPublisher.Close() })

	cmController, err := newFakeConfigMapController(t, clientset, "mdai")
	require.NoError(t, err)
	require.NotNil(t, cmController)
	t.Cleanup(func() { cmController.Stop() })

	opampServer, _ := opamp.NewOpAMPControlServer(zap.NewNop(), auditAdapter, eventPublisher)

	deps := HandlerDeps{
		Logger:              zap.NewNop(),
		ValkeyClient:        valkeyClient,
		AuditAdapter:        auditAdapter,
		EventPublisher:      eventPublisher,
		ConfigMapController: cmController,
		Deduper:             adapter.NewDeduper(),
		OpAMPServer:         opampServer,
	}
	return deps
}
