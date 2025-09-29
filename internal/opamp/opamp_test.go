package opamp

import (
	"context"
	"testing"

	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/decisiveai/mdai-data-core/eventing"
	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/stretchr/testify/assert"
	valkeymock "github.com/valkey-io/valkey-go/mock"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/mock/gomock"
	"go.uber.org/zap"
)

type OpampDeps struct {
	MockPublisher MockPublisher
	OpAmpServer   OpAMPControlServer
}

type MockPublisher struct {
	received *[]map[string]string
}

func (mockPublisher MockPublisher) Publish(ctx context.Context, event eventing.MdaiEvent, subject eventing.MdaiEventSubject) error {
	newReceived := map[string]string{
		"subject":  subject.String(),
		"name":     event.Name,
		"payload":  event.Payload,
		"source":   event.Source,
		"sourceId": event.SourceID,
		"hubName":  event.HubName,
	}
	*mockPublisher.received = append(*mockPublisher.received, newReceived)
	return nil
}

// (sigh) Linter, it's a mock method, okay?
// nolint:revive
func (mockPublisher MockPublisher) Close() error {
	return nil
}

func setupMocks(t *testing.T) OpampDeps {
	t.Helper()

	ctrl := gomock.NewController(t)
	valkeyClient := valkeymock.NewClient(ctrl)
	// We are not testing valkey here; that is a concern outside of our scope.
	valkeyClient.EXPECT().Do(gomock.Any(), gomock.Any()).Return(valkeymock.Result(valkeymock.ValkeyString("sgood"))).AnyTimes()
	auditAdapter := audit.NewAuditAdapter(zap.NewNop(), valkeyClient)

	eventPublisher := MockPublisher{
		received: &[]map[string]string{},
	}

	opampServer := NewOpAMPControlServer(zap.NewNop(), auditAdapter, eventPublisher)

	deps := OpampDeps{
		MockPublisher: eventPublisher,
		OpAmpServer:   *opampServer,
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
							Key:   instanceIDIdentifyingAttributeKey,
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "instance1"}}),
						},
					},
					NonIdentifyingAttributes: []*protobufs.KeyValue{
						{
							Key:   replayIDNonIdentifyingAttributeKey,
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
				instanceID: "instance1",
				replayID:   "replay1",
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
							Key:   instanceIDIdentifyingAttributeKey,
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "instance1"}}),
						},
						{
							Key:   replayIDNonIdentifyingAttributeKey,
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
							Key:   replayIDNonIdentifyingAttributeKey,
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
				instanceID: "instance1",
				replayID:   "replay1",
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

func MakeLogsWithAttributes(attributes map[string]any) plog.Logs {
	logs := plog.NewLogs()
	resourceLogs := logs.ResourceLogs().AppendEmpty()
	scopeLogs := resourceLogs.ScopeLogs().AppendEmpty()
	logRecord := scopeLogs.LogRecords().AppendEmpty()
	_ = logRecord.Attributes().FromRaw(attributes)
	return logs
}

func TestDigForCompletionAndExecuteHandler(t *testing.T) {
	deps := setupMocks(t)
	opampServer := deps.OpAmpServer
	agentID := "agent1"
	opampServer.agentUIDInfoMap = map[string]OpAMPAgent{
		agentID: {
			instanceID: "instance1",
			replayID:   "replay1",
			hubName:    "hub1",
		},
	}
	tests := []struct {
		description   string
		logs          plog.Logs
		expectedEvent map[string]string
	}{
		{
			description: "complete has all attributes",
			logs: MakeLogsWithAttributes(map[string]any{
				ingestStatusAttributeKey: ingestStatusCompleted,
			}),
			expectedEvent: map[string]string{
				"subject":  "replay.hub1.completed",
				"name":     "replay-complete",
				"payload":  `{"replay_id":"replay1","replay_result":"completed","replayer_instance_id":"instance1"}`,
				"source":   "buffer-replay",
				"sourceId": "instance1",
				"hubName":  "hub1",
			},
		},
		{
			description: "failure all attributes",
			logs: MakeLogsWithAttributes(map[string]any{
				ingestStatusAttributeKey: ingestStatusFailed,
			}),
			expectedEvent: map[string]string{
				"subject":  "replay.hub1.failed",
				"name":     "replay-complete",
				"payload":  `{"replay_id":"replay1","replay_result":"failed","replayer_instance_id":"instance1"}`,
				"source":   "buffer-replay",
				"sourceId": "instance1",
				"hubName":  "hub1",
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			opampServer.DigForCompletionAndPublish(t.Context(), agentID, tt.logs)
			assert.Contains(t, *(deps.MockPublisher.received), tt.expectedEvent)
		})
	}
}
