package models

import (
	"encoding/json"
	"testing"
)

func TestRuntimeProtocolJSON(t *testing.T) {
	var info DeviceInfoResponse
	if err := json.Unmarshal([]byte(`{"agent_protocol":"free","agent_runtime":{"mode":"local-research","backend":"rustshine","streamer_prepared":true}}`), &info); err != nil {
		t.Fatal(err)
	}
	if info.AgentProtocol != "free" || info.EffectiveAgentProtocol() != "local" {
		t.Fatal(info)
	}
	var legacy DeviceInfoResponse
	if err := json.Unmarshal([]byte(`{"agent_protocol":"free"}`), &legacy); err != nil {
		t.Fatal(err)
	}
	if legacy.EffectiveAgentProtocol() != "free" {
		t.Fatal(legacy)
	}
}
