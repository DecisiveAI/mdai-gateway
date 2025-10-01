package opamp

type opAMPRequestBody struct {
	InstanceUID string `json:"instance_uid"`
	Reason      string `json:"reason"`
}

type ReplayCompletionEventPayload struct {
	ReplayID           string `json:"replay_id"`
	ReplayResult       string `json:"replay_result"`
	ReplayerInstanceID string `json:"replayer_instance_id"`
}
