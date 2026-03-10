package opamp

import (
	"context"
	"testing"

	"github.com/mydecisive/mdai-data-core/audit"
	"github.com/mydecisive/mdai-data-core/eventing"
	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
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

	opampServer, _ := NewOpAMPControlServer(zap.NewNop(), auditAdapter, eventPublisher)

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
		expected    opAMPAgentInfo
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
			expected: opAMPAgentInfo{
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
							Key:   replayStatusVariableNonIdentifyingAttribute,
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_StringValue{StringValue: "replay-status"}}),
						},
						{
							Key:   "aklsdjf",
							Value: &(protobufs.AnyValue{Value: &protobufs.AnyValue_IntValue{IntValue: 1337}}),
						},
					},
				},
			},
			expected: opAMPAgentInfo{
				instanceID:           "instance1",
				replayID:             "replay1",
				hubName:              "hub1",
				replayStatusVariable: "replay-status",
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
			expected:    opAMPAgentInfo{},
			expectFound: false,
		},
		{
			description: "no agent description",
			msg:         &protobufs.AgentToServer{},
			expected:    opAMPAgentInfo{},
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
			expected:    opAMPAgentInfo{},
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
	tests := []struct {
		agentID       string
		agentInfoes   map[string]opAMPAgentInfo
		description   string
		logs          plog.Logs
		expectErr     bool
		expectedEvent map[string]string
	}{
		{
			agentID: "agent1",
			agentInfoes: map[string]opAMPAgentInfo{
				"agent1": {
					instanceID:           "instance1",
					replayID:             "replay1",
					hubName:              "hub1",
					replayStatusVariable: "replay-status",
				},
			},
			description: "complete has all attributes",
			logs: MakeLogsWithAttributes(map[string]any{
				ingestStatusAttributeKey: ingestStatusCompleted,
			}),
			expectedEvent: map[string]string{
				"subject":  "var.hub1.replay-status",
				"name":     "replay-complete",
				"payload":  `{"variableRef":"replay-status","dataType":"string","operation":"add","data":"{\"replay_name\":\"replay1\",\"replay_status\":\"completed\"}"}`,
				"source":   "manual_variables_api",
				"sourceId": "instance1",
				"hubName":  "hub1",
			},
		},
		{
			agentID: "agent1",
			agentInfoes: map[string]opAMPAgentInfo{
				"agent1": {
					instanceID:           "instance1",
					replayID:             "replay1",
					hubName:              "hub1",
					replayStatusVariable: "replay-status",
				},
			},
			description: "failure all attributes",
			logs: MakeLogsWithAttributes(map[string]any{
				ingestStatusAttributeKey: ingestStatusFailed,
			}),
			expectedEvent: map[string]string{
				"subject":  "var.hub1.replay-status",
				"name":     "replay-complete",
				"payload":  `{"variableRef":"replay-status","dataType":"string","operation":"add","data":"{\"replay_name\":\"replay1\",\"replay_status\":\"failed\"}"}`,
				"source":   "manual_variables_api",
				"sourceId": "instance1",
				"hubName":  "hub1",
			},
		},
		{
			agentID: "agent1",
			agentInfoes: map[string]opAMPAgentInfo{
				"agent1": {
					instanceID: "instance1",
					replayID:   "replay1",
					hubName:    "hub1",
				},
			},
			description: "missing status var attribute",
			logs: MakeLogsWithAttributes(map[string]any{
				ingestStatusAttributeKey: ingestStatusCompleted,
			}),
			expectErr: true,
		},
		{
			agentID: "agent1",
			agentInfoes: map[string]opAMPAgentInfo{
				"agent1": {
					instanceID: "instance1",
					replayID:   "replay1",
				},
			},
			description: "missing hub name",
			logs: MakeLogsWithAttributes(map[string]any{
				ingestStatusAttributeKey: ingestStatusFailed,
			}),
			expectErr: true,
		},
		{
			agentID: "agent1",
			agentInfoes: map[string]opAMPAgentInfo{
				"agent1": {
					instanceID: "instance1",
					hubName:    "hub1",
				},
			},
			description: "missing replay id",
			logs: MakeLogsWithAttributes(map[string]any{
				ingestStatusAttributeKey: ingestStatusFailed,
			}),
			expectErr: true,
		},
		{
			agentInfoes: map[string]opAMPAgentInfo{},
			description: "missing agent",
			logs: MakeLogsWithAttributes(map[string]any{
				ingestStatusAttributeKey: ingestStatusFailed,
			}),
			expectErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.description, func(t *testing.T) {
			opampServer.connectedAgents.setAgentDescription(tt.agentID, tt.agentInfoes[tt.agentID])
			err := opampServer.digForCompletionAndPublish(t.Context(), tt.agentID, tt.logs)
			if tt.expectErr {
				require.Error(t, err)
			} else {
				require.NoError(t, err)
				assert.Contains(t, *(deps.MockPublisher.received), tt.expectedEvent)
			}
		})
	}
}
