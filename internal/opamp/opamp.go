package opamp

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"

	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/decisiveai/mdai-data-core/eventing"
	"github.com/decisiveai/mdai-data-core/eventing/publisher"
	"github.com/decisiveai/mdai-gateway/internal/adapter"
	"github.com/decisiveai/mdai-gateway/internal/nats"
	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/server"
	"github.com/open-telemetry/opamp-go/server/types"
	"go.opentelemetry.io/collector/pdata/plog"
	"go.uber.org/zap"
)

const (
	s3ReceiverCapabilityKey            = "org.opentelemetry.collector.receiver.awss3"
	ingestStatusAttributeKey           = "ingest_status"
	ingestStatusCompleted              = "completed"
	ingestStatusFailed                 = "failed"
	replayIDNonIdentifyingAttributeKey = "replay_id"
	hubNameNonIdentifyingAttributeKey  = "hub_name"
	instanceIDIdentifyingAttributeKey  = "service.instance.id"
)

type OpAMPAgent struct {
	instanceID string
	replayID   string
	hubName    string
}

type OpAMPControlServer struct {
	logger         *zap.Logger
	auditAdapter   *audit.AuditAdapter
	eventPublisher publisher.Publisher

	agentConnections map[string]types.Connection
	agentUIDInfoMap  map[string]OpAMPAgent
	srv              server.OpAMPServer
	logUnmarshaler   plog.ProtoUnmarshaler
}

func NewOpAMPControlServer(logger *zap.Logger, auditAdapter *audit.AuditAdapter, eventPublisher publisher.Publisher) *OpAMPControlServer {
	ctrl := &OpAMPControlServer{
		logger:           logger,
		auditAdapter:     auditAdapter,
		eventPublisher:   eventPublisher,
		agentUIDInfoMap:  make(map[string]OpAMPAgent),
		agentConnections: make(map[string]types.Connection),
		srv:              server.New(nil),
		logUnmarshaler:   plog.ProtoUnmarshaler{},
	}
	return ctrl
}

func (ctrl *OpAMPControlServer) GetOpAMPHTTPHandler() (http.HandlerFunc, server.ConnContext, error) {
	settings := server.Settings{
		Callbacks: types.Callbacks{
			OnConnecting: func(r *http.Request) types.ConnectionResponse {
				return types.ConnectionResponse{
					Accept: true,
					ConnectionCallbacks: types.ConnectionCallbacks{
						OnMessage:         ctrl.OnMessage,
						OnConnectionClose: ctrl.OnDisconnect,
					},
				}
			},
		},
	}
	handler, connCtx, err := ctrl.srv.Attach(settings)
	return http.HandlerFunc(handler), connCtx, err
}

func (ctrl *OpAMPControlServer) OnMessage(ctx context.Context, conn types.Connection, msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
	uid := string(msg.GetInstanceUid())
	ctrl.agentConnections[uid] = conn

	foundAgent, ok := harvestAgentInfoesFromAgentDescription(msg)
	if ok {
		ctrl.agentUIDInfoMap[uid] = foundAgent
	}

	if msg.GetCustomMessage() != nil && msg.GetCustomMessage().GetCapability() == s3ReceiverCapabilityKey {
		ctrl.HandleS3ReceiverMessage(ctx, uid, msg)
	}
	return &protobufs.ServerToAgent{}
}

func harvestAgentInfoesFromAgentDescription(msg *protobufs.AgentToServer) (OpAMPAgent, bool) {
	agentDescription := msg.GetAgentDescription()
	if agentDescription == nil {
		return OpAMPAgent{}, false
	}

	agent := OpAMPAgent{}
	hasAgentAttributes := false
	for _, attr := range agentDescription.GetIdentifyingAttributes() {
		if attr.GetKey() == instanceIDIdentifyingAttributeKey {
			agent.instanceID = attr.GetValue().GetStringValue()
			hasAgentAttributes = true
		}
	}
	for _, attr := range agentDescription.GetNonIdentifyingAttributes() {
		if attr.GetKey() == replayIDNonIdentifyingAttributeKey {
			agent.replayID = attr.GetValue().GetStringValue()
			hasAgentAttributes = true
		}
		if attr.GetKey() == hubNameNonIdentifyingAttributeKey {
			agent.hubName = attr.GetValue().GetStringValue()
			hasAgentAttributes = true
		}
	}
	return agent, hasAgentAttributes
}

func (ctrl *OpAMPControlServer) HandleS3ReceiverMessage(ctx context.Context, agentID string, msg *protobufs.AgentToServer) {
	logMessage, err := ctrl.logUnmarshaler.UnmarshalLogs(msg.GetCustomMessage().GetData())
	if err != nil {
		ctrl.logger.Error("Failed to unmarshal OpAMP AWSS3 Receiver custom message logs.", zap.Error(err))
	}
	ctrl.DigForCompletionAndPublish(ctx, agentID, logMessage)
}

func (ctrl *OpAMPControlServer) DigForCompletionAndPublish(ctx context.Context, agentID string, logMessage plog.Logs) {
	foundCompletionLog := false
	resourceLogs := logMessage.ResourceLogs()
	for i := 0; i < resourceLogs.Len() && !foundCompletionLog; i++ {
		resourceLog := resourceLogs.At(i)
		scopeLogs := resourceLog.ScopeLogs()
		for j := 0; i < scopeLogs.Len() && !foundCompletionLog; i++ {
			scopeLog := scopeLogs.At(j)
			logRecords := scopeLog.LogRecords()
			rlen := logRecords.Len()
			for k := range rlen {
				logRecord := logRecords.At(k)
				attributes := logRecord.Attributes()
				if attribute, ok := attributes.Get(ingestStatusAttributeKey); ok {
					statusAttrValue := attribute.AsString()
					switch statusAttrValue {
					case ingestStatusCompleted:
						ctrl.PublishCompletionEvent(ctx, agentID, statusAttrValue)
						foundCompletionLog = true
					case ingestStatusFailed:
						ctrl.PublishCompletionEvent(ctx, agentID, statusAttrValue)
						foundCompletionLog = true
					}
				}
			}
		}
	}
}

type ReplayCompletionEventPayload struct {
	ReplayID           string `json:"replay_id"`
	ReplayResult       string `json:"replay_result"`
	ReplayerInstanceID string `json:"replayer_instance_id"`
}

func (ctrl *OpAMPControlServer) PublishCompletionEvent(ctx context.Context, agentID string, outcome string) {
	agent := ctrl.agentUIDInfoMap[agentID]
	subject := eventing.NewMdaiEventSubject(eventing.ReplayEventType, fmt.Sprintf("%s.%s", agent.hubName, outcome))

	if agent.hubName == "" {
		ctrl.logger.Error(
			"Got replay completion event but have no hubName for OpAMP agent! Unable to publish completion event!",
			zap.String("instanceId", agent.instanceID),
			zap.String("replayId", agent.replayID),
			zap.String("replayOutcome", outcome),
		)
		return
	}

	if agent.replayID == "" {
		ctrl.logger.Warn(
			"Got replay completion event but have no replay ID for OpAMP agent!",
			zap.String("instanceId", agent.instanceID),
			zap.String("hubName", agent.hubName),
			zap.String("replayOutcome", outcome),
		)
	}

	payload := ReplayCompletionEventPayload{
		ReplayID:           agent.replayID,
		ReplayResult:       outcome,
		ReplayerInstanceID: agent.instanceID,
	}
	payloadBytes, err := json.Marshal(payload)
	if err != nil {
		ctrl.logger.Error("Failed to marshal Replay Completion Event Payload.", zap.Error(err))
	}
	event := eventing.MdaiEvent{
		Name:     "replay-complete",
		Source:   eventing.BufferReplaySource,
		SourceID: agent.instanceID,
		Payload:  string(payloadBytes),
		HubName:  agent.hubName,
	}
	event.ApplyDefaults()
	eventsPerSubject := []adapter.EventPerSubject{
		{
			Event:   event,
			Subject: subject,
		},
	}
	if _, err := nats.PublishEvents(ctx, ctrl.logger, ctrl.eventPublisher, eventsPerSubject, ctrl.auditAdapter); err != nil {
		ctrl.logger.Error(
			"Failed to publish replay completion event",
			zap.Error(err),
			zap.String("instanceId", agent.instanceID),
			zap.String("replayId", agent.replayID),
			zap.String("replayOutcome", outcome),
			zap.String("subject", subject.String()),
		)
	}
}

func (ctrl *OpAMPControlServer) OnDisconnect(conn types.Connection) {
	for uid, agent := range ctrl.agentConnections {
		if agent == conn {
			delete(ctrl.agentConnections, uid)
			break
		}
	}
}

func (ctrl *OpAMPControlServer) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "method not allowed", http.StatusMethodNotAllowed)
		return
	}
	type reqBody struct {
		InstanceUID string `json:"instance_uid"`
		Reason      string `json:"reason"`
	}
	var body reqBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		http.Error(w, "invalid body", http.StatusBadRequest)
		return
	}

	_, ok := ctrl.agentConnections[body.InstanceUID]
	if !ok {
		http.Error(w, "agent not connected", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
