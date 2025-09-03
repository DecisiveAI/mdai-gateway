package opamp

import (
	"context"
	"encoding/json"
	"go.opentelemetry.io/collector/pdata/plog"
	"log"
	"net/http"

	"github.com/open-telemetry/opamp-go/protobufs"
	"github.com/open-telemetry/opamp-go/server"
	"github.com/open-telemetry/opamp-go/server/types"
)

type OpAMPControlServer struct {
	agents         map[string]types.Connection
	srv            server.OpAMPServer
	logUnmarshaler plog.ProtoUnmarshaler
}

func NewOpAMPControlServer() *OpAMPControlServer {
	ctrl := &OpAMPControlServer{
		agents:         make(map[string]types.Connection),
		srv:            server.New(nil),
		logUnmarshaler: plog.ProtoUnmarshaler{},
	}
	return ctrl
}

func (ctrl *OpAMPControlServer) GetHandler() (http.HandlerFunc, server.ConnContext, error) {
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
	ctrl.agents[uid] = conn
	if msg.CustomMessage != nil && msg.CustomMessage.Capability == "org.opentelemetry.collector.receiver.awss3" {
		ctrl.HandleS3ReceiverMessage(msg)
	}
	return &protobufs.ServerToAgent{}
}

func (ctrl *OpAMPControlServer) HandleS3ReceiverMessage(msg *protobufs.AgentToServer) {
	logMessage, err := ctrl.logUnmarshaler.UnmarshalLogs(msg.CustomMessage.Data)
	if err != nil {
		log.Printf("Failed to unmarshal OpAMP custom message logs: %v", err)
	}
	DigForCompletionAndExecuteHandler(logMessage)
}

func DigForCompletionAndExecuteHandler(logMessage plog.Logs) {
	resourceLogs := logMessage.ResourceLogs().All()
	for _, resourceLog := range resourceLogs {
		scopeLogs := resourceLog.ScopeLogs().All()
		for _, scopeLog := range scopeLogs {
			logRecords := scopeLog.LogRecords().All()
			for _, logRecord := range logRecords {
				attributes := logRecord.Attributes().All()
				for key, val := range attributes {
					if key == "ingest_status" && val.AsString() == "completed" {
						log.Printf("%s = %s\n", key, val.AsString())
						// Do the execute handler part
					}
				}
			}
		}
	}
}

func (ctrl *OpAMPControlServer) OnDisconnect(conn types.Connection) {
	for uid, c := range ctrl.agents {
		if c == conn {
			delete(ctrl.agents, uid)
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

	_, ok := ctrl.agents[body.InstanceUID]
	if !ok {
		http.Error(w, "agent not connected", http.StatusNotFound)
		return
	}

	w.WriteHeader(http.StatusNoContent)
}
