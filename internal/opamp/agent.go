package opamp

import (
	"github.com/open-telemetry/opamp-go/server/types"
	"sync"
)

type opAMPAgentInfo struct {
	instanceID string
	replayID   string
	hubName    string
	connection types.Connection
}

type opAMPConnectedAgents struct {
	mu          sync.Mutex
	agentInfoes map[string]opAMPAgentInfo
	// connections are not 1:1 with agentInfoes. A single agent will recycle many connections, so we're tracking them separately for now
	connections map[string]types.Connection
}

func newOpAMPConnectedAgents() *opAMPConnectedAgents {
	return &opAMPConnectedAgents{
		agentInfoes: make(map[string]opAMPAgentInfo),
		connections: make(map[string]types.Connection),
	}
}

func (ca *opAMPConnectedAgents) setAgentDescription(id string, agent opAMPAgentInfo) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	ca.agentInfoes[id] = agent
}

func (ca *opAMPConnectedAgents) getAgentDescription(id string) (opAMPAgentInfo, bool) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	agent, ok := ca.agentInfoes[id]
	return agent, ok
}

func (ca *opAMPConnectedAgents) setConnection(id string, connection types.Connection) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	ca.connections[id] = connection
}

func (ca *opAMPConnectedAgents) getConnection(id string) (types.Connection, bool) {
	ca.mu.Lock()
	defer ca.mu.Unlock()
	connection, ok := ca.connections[id]
	return connection, ok
}

func (ca *opAMPConnectedAgents) disconnectAgentsForConnection(conn types.Connection) {
	ca.mu.Lock()
	defer ca.mu.Unlock()

	for uid, agent := range ca.connections {
		if agent == conn {
			delete(ca.connections, uid)
			// TODO: Figure out how to clean up agentInfoes. Currently this lives longer than the connection, but there's currently no clean, obvious place to remove them. They could still send OpAMP messages after finishing, in theory
			break
		}
	}
}
