package models

func (r *DeviceInfoResponse) EffectiveAgentProtocol() string {
	if r == nil {
		return ""
	}
	return r.AgentRuntime.EffectiveProtocol(r.AgentProtocol)
}
func (r *StatusData) EffectiveAgentProtocol() string {
	if r == nil {
		return ""
	}
	return r.AgentRuntime.EffectiveProtocol(r.AgentProtocol)
}
