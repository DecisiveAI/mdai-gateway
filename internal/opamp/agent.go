package opamp

import (
	"sync"
)

type opAMPAgentInfo struct {
	instanceID string
	replayID   string
	hubName    string
}

type opAMPConnectedAgents struct {
	mu          sync.Mutex
	agentInfoes map[string]opAMPAgentInfo
}

func newOpAMPConnectedAgents() *opAMPConnectedAgents {
	return &opAMPConnectedAgents{
		agentInfoes: make(map[string]opAMPAgentInfo),
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
