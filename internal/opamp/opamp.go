package opamp

import (
	"context"
	"encoding/json"
	"fmt"
	"github.com/decisiveai/mdai-data-core/audit"
	"github.com/decisiveai/mdai-data-core/eventing"
	"github.com/decisiveai/mdai-data-core/eventing/publisher"
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
)

type OpAMPControlServer struct {
	logger         *zap.Logger
	auditAdapter   *audit.AuditAdapter
	eventPublisher publisher.Publisher

	agentConnections   map[string]types.Connection
	agentUIDToReplayId map[string]string
	srv                server.OpAMPServer
	logUnmarshaler     plog.ProtoUnmarshaler
}

func NewOpAMPControlServer(logger *zap.Logger, auditAdapter *audit.AuditAdapter, eventPublisher publisher.Publisher) *OpAMPControlServer {
	ctrl := &OpAMPControlServer{
		logger:             logger,
		auditAdapter:       auditAdapter,
		eventPublisher:     eventPublisher,
		agentUIDToReplayId: make(map[string]string),
		agentConnections:   make(map[string]types.Connection),
		srv:                server.New(nil),
		logUnmarshaler:     plog.ProtoUnmarshaler{},
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
		ctrl.TryHarvestReplayIdFromAgentDescription(msg, uid)
	}

	if msg.CustomMessage != nil && msg.CustomMessage.Capability == s3ReceiverCapabilityKey {
		ctrl.HandleS3ReceiverMessage(ctx, uid, msg)
	}
	return &protobufs.ServerToAgent{}
}

func (ctrl *OpAMPControlServer) TryHarvestReplayIdFromAgentDescription(msg *protobufs.AgentToServer, uid string) {
	for _, attr := range (*msg.AgentDescription).NonIdentifyingAttributes {
		if attr.GetKey() == replayIdNonIdentifyingAttributeKey {
			ctrl.agentUIDToReplayId[uid] = attr.GetValue().GetStringValue()
		}
	}
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
						ctrl.logger.Info("Replay DONE", zap.String("replay_id", ctrl.agentUIDToReplayId[agentID]))
						// Do the execute handler part
						subject := "replay.success"
						event := eventing.MdaiEvent{
							Name:     subject,
							Source:   "Replay Collector",
							SourceID: agentID,
							Payload:  fmt.Sprintf(`{"replayId": "%s"}`, ctrl.agentUIDToReplayId[agentID]),
						}
						event.ApplyDefaults()
						if err := ctrl.eventPublisher.Publish(ctx, event, subject); err != nil {
							ctrl.logger.Error("Failed to publish event", zap.Error(err), zap.String("replay_id", ctrl.agentUIDToReplayId[agentID]), zap.String("subject", subject))
						}

						continue
					}
				}
			}
		}
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
