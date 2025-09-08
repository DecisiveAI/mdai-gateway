package opamp

import (
	"context"
	"encoding/json"
	"fmt"
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
	"net/http"
)

const (
	s3ReceiverCapabilityKey            = "org.opentelemetry.collector.receiver.awss3"
	ingestStatusAttributeKey           = "ingest_status"
	ingestStatusCompleted              = "completed"
	replayIdNonIdentifyingAttributeKey = "replay_id"
	hubNameNonIdentifyingAttributeKey  = "hub_name"
	instanceIdIdentifyingAttributeKey  = "service.instance.id"
)

type OpAMPAgent struct {
	instanceId string
	replayId   string
	hubName    string
}

type OpAMPControlServer struct {
	logger         *zap.Logger
	auditAdapter   *audit.AuditAdapter
	eventPublisher publisher.Publisher

	agentConnections map[string]types.Connection
	agentUidInfoMap  map[string]OpAMPAgent
	srv              server.OpAMPServer
	logUnmarshaler   plog.ProtoUnmarshaler
}

func NewOpAMPControlServer(logger *zap.Logger, auditAdapter *audit.AuditAdapter, eventPublisher publisher.Publisher) *OpAMPControlServer {
	ctrl := &OpAMPControlServer{
		logger:           logger,
		auditAdapter:     auditAdapter,
		eventPublisher:   eventPublisher,
		agentUidInfoMap:  make(map[string]OpAMPAgent),
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
	uid := string(msg.InstanceUid)
	ctrl.agentConnections[uid] = conn

	if msg.AgentDescription != nil {
		ctrl.TryHarvestAgentInfoesFromAgentDescription(msg, uid)
	}

	if msg.CustomMessage != nil && msg.CustomMessage.Capability == s3ReceiverCapabilityKey {
		ctrl.HandleS3ReceiverMessage(ctx, uid, msg)
	}
	return &protobufs.ServerToAgent{}
}

func (ctrl *OpAMPControlServer) TryHarvestAgentInfoesFromAgentDescription(msg *protobufs.AgentToServer, uid string) {
	agent := OpAMPAgent{}
	for _, attr := range (*msg.AgentDescription).IdentifyingAttributes {
		if attr.GetKey() == instanceIdIdentifyingAttributeKey {
			agent.instanceId = attr.GetValue().GetStringValue()
		}
	}
	for _, attr := range (*msg.AgentDescription).NonIdentifyingAttributes {
		if attr.GetKey() == replayIdNonIdentifyingAttributeKey {
			agent.replayId = attr.GetValue().GetStringValue()
		}
		if attr.GetKey() == hubNameNonIdentifyingAttributeKey {
			agent.hubName = attr.GetValue().GetStringValue()
		}
	}
	ctrl.agentUidInfoMap[uid] = agent
}

func (ctrl *OpAMPControlServer) HandleS3ReceiverMessage(ctx context.Context, agentID string, msg *protobufs.AgentToServer) {
	logMessage, err := ctrl.logUnmarshaler.UnmarshalLogs(msg.CustomMessage.Data)
	if err != nil {
		ctrl.logger.Error("Failed to unmarshal OpAMP AWSS3 Receiver custom message logs.", zap.Error(err))
	}
	ctrl.TryDigForCompletionAndExecuteHandler(ctx, agentID, logMessage)
}

func (ctrl *OpAMPControlServer) TryDigForCompletionAndExecuteHandler(ctx context.Context, agentID string, logMessage plog.Logs) {
	resourceLogs := logMessage.ResourceLogs().All()
	for _, resourceLog := range resourceLogs {
		scopeLogs := resourceLog.ScopeLogs().All()
		for _, scopeLog := range scopeLogs {
			logRecords := scopeLog.LogRecords().All()
			for _, logRecord := range logRecords {
				attributes := logRecord.Attributes().All()
				for key, val := range attributes {
					if key == ingestStatusAttributeKey && val.AsString() == ingestStatusCompleted {
						ctrl.PublishCompletionEvent(ctx, agentID)
						continue
					}
				}
			}
		}
	}
}

func (ctrl *OpAMPControlServer) PublishCompletionEvent(ctx context.Context, agentID string) {
	agent := ctrl.agentUidInfoMap[agentID]
	subject := eventing.NewMdaiEventSubject(eventing.ReplayEventType, fmt.Sprintf("%s.%s", agent.hubName, "complete"))
	event := eventing.MdaiEvent{
		Name:     "Replay Complete",
		Source:   eventing.BufferReplaySource,
		SourceID: agent.instanceId,
		Payload:  fmt.Sprintf(`{"replayId": "%s"}`, agent.replayId),
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
			"Failed to publish event",
			zap.Error(err),
			zap.String("replay_id", agent.replayId),
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
