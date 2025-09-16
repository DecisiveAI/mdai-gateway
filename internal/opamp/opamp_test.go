package opamp

import (
	"context"
	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/decisiveai/mdai-data-core/eventing/publisher"
	natsserver "github.com/nats-io/nats-server/v2/server"
	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
	"testing"
	"time"
)

type OpampDeps struct {
	Logger         *zap.Logger
	EventPublisher publisher.Publisher
	OpAmpServer    OpAMPControlServer
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

func setupMocks(t *testing.T) OpampDeps {
	t.Helper()

	ctrl := gomock.NewController(t)
	valkeyClient := valkeymock.NewClient(ctrl)
	auditAdapter := audit.NewAuditAdapter(zap.NewNop(), valkeyClient)

	srv := runJetStream(t)
	t.Cleanup(func() { srv.Shutdown() })
	eventPublisher, err := publisher.NewPublisher(context.Background(), zap.NewNop(), "foo-publisher")
	require.NoError(t, err)
	t.Cleanup(func() { _ = eventPublisher.Close() })

	opampServer := NewOpAMPControlServer(zap.NewNop(), auditAdapter, eventPublisher)
	//opampHandler, _, err := opampServer.GetOpAMPHTTPHandler()

	deps := OpampDeps{
		Logger:         zap.NewNop(),
		EventPublisher: eventPublisher,
		OpAmpServer:    *opampServer,
	}
	return deps
}

func TestHarvestAgentInfoesFromAgentDescription(t *testing.T) {
	tests := []struct {
		description string
		msg         *protobufs.AgentToServer
		expected    OpAMPAgent
		expectFound bool
	}{
		{
			description: "has all attributes",
			msg: &protobufs.AgentToServer{
				AgentDescription: &protobufs.AgentDescription{
					IdentifyingAttributes: []*protobufs.KeyValue{
						{
							Key:   instanceIdIdentifyingAttributeKey,
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "instance1"}}),
						},
					},
					NonIdentifyingAttributes: []*protobufs.KeyValue{
						{
							Key:   replayIdNonIdentifyingAttributeKey,
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "replay1"}}),
						},
						{
							Key:   hubNameNonIdentifyingAttributeKey,
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "hub1"}}),
						},
					},
				},
			},
			expected: OpAMPAgent{
				instanceId: "instance1",
				replayId:   "replay1",
				hubName:    "hub1",
			},
			expectFound: true,
		},
		{
			description: "has all attributes and then some",
			msg: &protobufs.AgentToServer{
				AgentDescription: &protobufs.AgentDescription{
					IdentifyingAttributes: []*protobufs.KeyValue{
						{
							Key:   instanceIdIdentifyingAttributeKey,
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "instance1"}}),
						},
						{
							Key:   replayIdNonIdentifyingAttributeKey,
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "replay1"}}),
						},
						{
							Key:   "foobar",
							Value: nil,
						},
						{
							Key:   "barbaz",
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "bazfoo"}}),
						},
					},
					NonIdentifyingAttributes: []*protobufs.KeyValue{
						{
							Key:   hubNameNonIdentifyingAttributeKey,
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "hub1"}}),
						},
						{
							Key:   replayIdNonIdentifyingAttributeKey,
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "replay1"}}),
						},
						{
							Key:   "aklsdjf",
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_IntValue{IntValue: 1337}}),
						},
					},
				},
			},
			expected: OpAMPAgent{
				instanceId: "instance1",
				replayId:   "replay1",
				hubName:    "hub1",
			},
			expectFound: true,
		},
		{
			description: "no attributes",
			msg: &protobufs.AgentToServer{
				AgentDescription: &protobufs.AgentDescription{
					IdentifyingAttributes:    []*protobufs.KeyValue{},
					NonIdentifyingAttributes: []*protobufs.KeyValue{},
				},
			},
			expected:    OpAMPAgent{},
			expectFound: false,
		},
		{
			description: "no agent description",
			msg:         &protobufs.AgentToServer{},
			expected:    OpAMPAgent{},
			expectFound: false,
		},
		{
			description: "no applicable attributes",
			msg: &protobufs.AgentToServer{
				AgentDescription: &protobufs.AgentDescription{
					IdentifyingAttributes: []*protobufs.KeyValue{
						{
							Key:   "foobar",
							Value: nil,
						},
						{
							Key:   "barbaz",
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "bazfoo"}}),
						},
					},
					NonIdentifyingAttributes: []*protobufs.KeyValue{
						{
							Key:   "aklsdjf",
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_IntValue{IntValue: 1337}}),
						},
					},
				},
			},
			expected:    OpAMPAgent{},
			expectFound: false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			actual, foundAgent := harvestAgentInfoesFromAgentDescription(tt.msg)
			assert.Equal(t, tt.expected, actual)
			assert.Equal(t, tt.expectFound, foundAgent)
		})
	}
}

