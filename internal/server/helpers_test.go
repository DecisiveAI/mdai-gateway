package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"sync/atomic"
	"testing"
	"time"

	"github.com/mydecisive/mdai-data-core/audit"
	"github.com/mydecisive/mdai-data-core/eventing"
	"github.com/mydecisive/mdai-data-core/eventing/publisher"
	datacorekube "github.com/mydecisive/mdai-data-core/kube"
	datacorevariables "github.com/mydecisive/mdai-data-core/variables"
	"github.com/mydecisive/mdai-gateway/internal/adapter"
	"github.com/mydecisive/mdai-gateway/internal/opamp"
	"github.com/mydecisive/mdai-gateway/internal/variables"
	"github.com/stretchr/testify/require"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	corev1 "k8s.io/api/core/v1"
	metav1 "k8s.io/apimachinery/pkg/apis/meta/v1"
	"k8s.io/apimachinery/pkg/runtime"
	"k8s.io/client-go/kubernetes"
	"k8s.io/client-go/kubernetes/fake"
)

type fakePublisher struct{}

func (fakePublisher) Publish(context.Context, eventing.MdaiEvent, eventing.MdaiEventSubject) error {
	return nil
}

func (fakePublisher) Close() error {
	return nil
}

var _ publisher.Publisher = fakePublisher{}

// togglePublisher fails every Publish while fail is set, simulating a NATS outage.
type togglePublisher struct {
	fail atomic.Bool
}

func (p *togglePublisher) Publish(context.Context, eventing.MdaiEvent, eventing.MdaiEventSubject) error {
	if p.fail.Load() {
		return errors.New("nats unavailable")
	}
	return nil
}

func (*togglePublisher) Close() error { return nil }

var _ publisher.Publisher = (*togglePublisher)(nil)

const testSlowValueReadThreshold = 250 * time.Millisecond

func sampleHubVariablesSchemaRaw() map[string]string {
	return map[string]string{
		"data_boolean":       `{"type":"manual","dataType":"boolean","storageType":"mdai-valkey","serializeAs":[{"name":"DATA_BOOLEAN"}]}`,
		"data_map":           `{"type":"manual","dataType":"map","storageType":"mdai-valkey","serializeAs":[{"name":"DATA_MAP"}]}`,
		"data_set":           `{"type":"manual","dataType":"set","storageType":"mdai-valkey","serializeAs":[{"name":"DATA_SET","transformers":[{"type":"join","join":{"delimiter":"|"}}]}]}`,
		"data_string":        `{"type":"manual","dataType":"string","storageType":"mdai-valkey","serializeAs":[{"name":"DATA_STRING"}]}`,
		"data_int":           `{"type":"manual","dataType":"int","storageType":"mdai-valkey","serializeAs":[{"name":"DATA_INT"}]}`,
		"computed_string":    `{"type":"computed","dataType":"string","storageType":"mdai-valkey","serializeAs":[{"name":"COMPUTED_STRING"}]}`,
		"meta_hash_set":      `{"type":"meta","dataType":"metaHashSet","storageType":"mdai-valkey","variableRefs":["data_string","data_set"],"serializeAs":[{"name":"META_HASH_SET"}]}`,
		"meta_priority_list": `{"type":"meta","dataType":"metaPriorityList","storageType":"mdai-valkey","variableRefs":["data_string","data_set"],"serializeAs":[{"name":"META_PRIORITY_LIST","transformers":[{"type":"join","join":{"delimiter":"|"}}]}]}`,
	}
}

func newFakeClientset(t *testing.T) kubernetes.Interface { //nolint:ireturn
	t.Helper()

	scheme := runtime.NewScheme()
	_ = corev1.AddToScheme(scheme)

	configMap := corev1.ConfigMap{
		ObjectMeta: metav1.ObjectMeta{
			Name:      "mdaihub-sample-variables-schema",
			Namespace: "mdai",
			Labels: map[string]string{
				datacorekube.ConfigMapTypeLabel: datacorekube.VariablesSchemaMapType,
				datacorekube.LabelMdaiHubName:   "mdaihub-sample",
			},
		},
		Data: sampleHubVariablesSchemaRaw(),
	}

	return fake.NewClientset(&configMap)
}

func newFakeConfigMapController(t *testing.T, clientset kubernetes.Interface, namespace string) (*datacorekube.HubConfigMapController, error) {
	t.Helper()
	c, err := datacorekube.NewHubConfigMapController([]string{datacorekube.VariablesSchemaMapType}, namespace, clientset, zap.NewNop())
	if err != nil {
		return nil, err
	}

	if err := c.Run(); err != nil {
		t.Errorf("Controller failed to run: %v", err)
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

func expectedHubVariablesSchema(t *testing.T) variables.HubVariables {
	t.Helper()

	decoded, err := variables.DecodeHub(sampleHubVariablesSchemaRaw())
	require.NoError(t, err)

	return decoded
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

func setupMocks(t *testing.T, clientset kubernetes.Interface) HandlerDeps {
	t.Helper()

	ctrl := gomock.NewController(t)
	valkeyClient := valkeymock.NewClient(ctrl)
	logger := zap.NewNop()
	variableReader := datacorevariables.NewValkeyAdapter(valkeyClient, logger)
	auditAdapter := audit.NewAuditAdapter(zap.NewNop(), valkeyClient)
	eventPublisher := fakePublisher{}

	cmController, err := newFakeConfigMapController(t, clientset, "mdai")
	require.NoError(t, err)
	require.NotNil(t, cmController)
	t.Cleanup(func() { cmController.Stop() })

	opampServer, _ := opamp.NewOpAMPControlServer(zap.NewNop(), auditAdapter, eventPublisher)

	deps := HandlerDeps{
		Logger:                 logger,
		ValkeyClient:           valkeyClient,
		VariableReader:         variableReader,
		SlowValueReadThreshold: testSlowValueReadThreshold,
		AuditAdapter:           auditAdapter,
		EventPublisher:         eventPublisher,
		ConfigMapController:    cmController,
		Deduper:                adapter.NewDeduper(),
		OpAMPServer:            opampServer,
	}
	return deps
}

func setupReadOnlyMocks(t *testing.T, clientset kubernetes.Interface) HandlerDeps {
	t.Helper()

	ctrl := gomock.NewController(t)
	valkeyClient := valkeymock.NewClient(ctrl)
	logger := zap.NewNop()
	variableReader := datacorevariables.NewValkeyAdapter(valkeyClient, logger)
	auditAdapter := audit.NewAuditAdapter(zap.NewNop(), valkeyClient)

	cmController, err := newFakeConfigMapController(t, clientset, "mdai")
	require.NoError(t, err)
	require.NotNil(t, cmController)
	t.Cleanup(func() { cmController.Stop() })

	return HandlerDeps{
		Logger:                 logger,
		ValkeyClient:           valkeyClient,
		VariableReader:         variableReader,
		SlowValueReadThreshold: testSlowValueReadThreshold,
		AuditAdapter:           auditAdapter,
		ConfigMapController:    cmController,
		Deduper:                adapter.NewDeduper(),
		OpAMPServer: &opamp.OpAMPControlServer{
			HandlerFunc: func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(http.StatusNotImplemented)
			},
		},
	}
}
