package opamp

type ReplayCompletionEventPayload struct {
	ReplayID             string `json:"replay_id"`
	ReplayResult         string `json:"replay_result"`
	ReplayerInstanceID   string `json:"replayer_instance_id"`
	ReplayStatusVariable string `json:"replay_status_variable"`
}
