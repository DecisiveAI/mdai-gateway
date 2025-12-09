package opamp

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"github.com/google/uuid"
	"net/http"
	"os"
	"slices"

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
	s3ReceiverCapabilityKey                     = "org.opentelemetry.collector.receiver.awss3"
	ingestStatusAttributeKey                    = "ingest_status"
	ingestStatusCompleted                       = "completed"
	ingestStatusFailed                          = "failed"
	replayIDNonIdentifyingAttributeKey          = "replay_id"
	hubNameNonIdentifyingAttributeKey           = "hub_name"
	instanceIDIdentifyingAttributeKey           = "service.instance.id"
	replayStatusVariableNonIdentifyingAttribute = "replay_status_variable"
)

type OpAMPControlServer struct {
	logger         *zap.Logger
	auditAdapter   *audit.AuditAdapter
	eventPublisher publisher.Publisher

	connectedAgents *opAMPConnectedAgents
	srv             server.OpAMPServer
	logUnmarshaler  plog.ProtoUnmarshaler

	HandlerFunc http.HandlerFunc
	ConnContext server.ConnContext
}

func NewOpAMPControlServer(logger *zap.Logger, auditAdapter *audit.AuditAdapter, eventPublisher publisher.Publisher) (*OpAMPControlServer, error) {
	opampServer := server.New(nil)
	ctrl := &OpAMPControlServer{
		logger:          logger,
		auditAdapter:    auditAdapter,
		eventPublisher:  eventPublisher,
		connectedAgents: newOpAMPConnectedAgents(),
		srv:             opampServer,
		logUnmarshaler:  plog.ProtoUnmarshaler{},
	}
	settings := server.Settings{
		Callbacks: types.Callbacks{
			OnConnecting: func(r *http.Request) types.ConnectionResponse {
				return types.ConnectionResponse{
					Accept: true,
					ConnectionCallbacks: types.ConnectionCallbacks{
						OnMessage: ctrl.onMessage,
					},
				}
			},
		},
	}
	handler, connCtx, err := opampServer.Attach(settings)
	ctrl.ConnContext = connCtx
	ctrl.HandlerFunc = http.HandlerFunc(handler)

	return ctrl, err
}

// TODO: Write tests for this if it sticks around in this form.
func (ctrl *OpAMPControlServer) onMessage(ctx context.Context, conn types.Connection, msg *protobufs.AgentToServer) *protobufs.ServerToAgent {
	theUUID, err := uuid.FromBytes(msg.GetInstanceUid())
	if err != nil {
		ctrl.logger.Warn("failed to parse instance uid", zap.Error(err))
		return &protobufs.ServerToAgent{ErrorResponse: &protobufs.ServerErrorResponse{ErrorMessage: err.Error()}}
	}

	if foundAgent, ok := harvestAgentInfoesFromAgentDescription(msg); ok {
		ctrl.connectedAgents.setAgentDescription(theUUID.String(), foundAgent)
	}

	if msg.GetCustomMessage() != nil && msg.GetCustomMessage().GetCapability() == s3ReceiverCapabilityKey {
		if err := ctrl.handleS3ReceiverMessage(ctx, theUUID.String(), msg); err != nil {
			ctrl.logger.Warn("Failed to handle S3 receiver message", zap.Error(err))
		}
	}

	configFileBytes, err := os.ReadFile("/Users/trent.vigar/src/mdai-gateway/collector_config.yaml")
	if err != nil {
		ctrl.logger.Warn("Failed to read config file", zap.Error(err))
		return &protobufs.ServerToAgent{ErrorResponse: &protobufs.ServerErrorResponse{ErrorMessage: err.Error()}}
	}

	hasher := sha256.New()
	_, err = hasher.Write(configFileBytes)
	if err != nil {
		ctrl.logger.Warn("Failed to hash config file", zap.Error(err))
		return &protobufs.ServerToAgent{ErrorResponse: &protobufs.ServerErrorResponse{ErrorMessage: err.Error()}}
	}

	return &protobufs.ServerToAgent{
		RemoteConfig: &protobufs.AgentRemoteConfig{
			Config: &protobufs.AgentConfigMap{
				ConfigMap: map[string]*protobufs.AgentConfigFile{
					"thing": {
						Body:        configFileBytes,
						ContentType: "text/yaml",
					},
				},
			},
			ConfigHash: hasher.Sum(nil),
		},
	}
}

func harvestAgentInfoesFromAgentDescription(msg *protobufs.AgentToServer) (opAMPAgentInfo, bool) {
	agentDescription := msg.GetAgentDescription()
	if agentDescription == nil {
		return opAMPAgentInfo{}, false
	}

	agent := opAMPAgentInfo{}
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
		if attr.GetKey() == replayStatusVariableNonIdentifyingAttribute {
			agent.replayStatusVariable = attr.GetValue().GetStringValue()
			hasAgentAttributes = true
		}
	}
	return agent, hasAgentAttributes
}

func (ctrl *OpAMPControlServer) handleS3ReceiverMessage(ctx context.Context, agentID string, msg *protobufs.AgentToServer) error {
	logMessage, err := ctrl.logUnmarshaler.UnmarshalLogs(msg.GetCustomMessage().GetData())
	if err != nil {
		ctrl.logger.Error("Failed to unmarshal OpAMP AWSS3 Receiver custom message logs.", zap.Error(err))
		return err
	}
	return ctrl.digForCompletionAndPublish(ctx, agentID, logMessage)
}

func (ctrl *OpAMPControlServer) digForCompletionAndPublish(ctx context.Context, agentID string, log plog.Logs) error {
	completionStatuses := []string{ingestStatusCompleted, ingestStatusFailed}
	for _, resourceLog := range log.ResourceLogs().All() {
		for _, scopeLog := range resourceLog.ScopeLogs().All() {
			for _, logRecord := range scopeLog.LogRecords().All() {
				if attribute, ok := logRecord.Attributes().Get(ingestStatusAttributeKey); ok {
					statusAttrValue := attribute.AsString()
					if slices.Contains(completionStatuses, statusAttrValue) {
						return ctrl.publishCompletionEvent(ctx, agentID, statusAttrValue)
					}
				}
			}
		}
	}
	return nil
}

type ReplayCompletion struct {
	ReplayName   string `json:"replay_name"`
	ReplayStatus string `json:"replay_status"`
}

func (ctrl *OpAMPControlServer) publishCompletionEvent(ctx context.Context, agentID string, statusAttrValue string) error {
	agent, ok := ctrl.connectedAgents.getAgentDescription(agentID)
	if !ok {
		return errors.New("unknown agent")
	}
	if agent.hubName == "" {
		return errors.New("missing hubName")
	}
	if agent.replayID == "" {
		return errors.New("missing replay ID")
	}
	if agent.replayStatusVariable == "" {
		return errors.New("missing replay status variable ref")
	}

	// Typical subject eventing.var.mdaihub-sample.replay_a_request
	subject := eventing.NewMdaiEventSubject(eventing.VarEventType, fmt.Sprintf("%s.%s", agent.hubName, agent.replayStatusVariable))

	dataObj := ReplayCompletion{
		ReplayName:   agent.replayID,
		ReplayStatus: statusAttrValue,
	}
	dataObjJSON, err := json.Marshal(dataObj)
	if err != nil {
		return err
	}
	ctrl.logger.Info("Publishing replay completion event", zap.String("subject", subject.String()), zap.String("event", string(dataObjJSON)))
	payload := eventing.VariablesActionPayload{
		VariableRef: agent.replayStatusVariable,
		DataType:    "string",
		Operation:   "add",
		Data:        string(dataObjJSON),
	}
	payloadBytes, marshalErr := json.Marshal(payload)
	if marshalErr != nil {
		ctrl.logger.Error("Failed to marshal Replay Completion Event Payload.", zap.Error(marshalErr))
	}
	event := eventing.MdaiEvent{
		Name:     "replay-complete",
		Source:   eventing.ManualVariablesEventSource,
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
	_, publishErr := nats.PublishEvents(ctx, ctrl.logger, ctrl.eventPublisher, eventsPerSubject, ctrl.auditAdapter)
	return publishErr
}
