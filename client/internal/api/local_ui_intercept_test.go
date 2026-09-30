package api

import (
	"encoding/json"
	"testing"
)

// withLocalUIEnabled toggles globalLocalUI.enabled for the duration of one
// test, restoring it afterward -- injectLocalUIParseTool only reads
// .enabled (not .parser), so tests can exercise it without a real
// *localui.Parser (which needs ONNX models on disk to construct).
func withLocalUIEnabled(t *testing.T, enabled bool) {
	t.Helper()
	globalLocalUI.mu.Lock()
	prev := globalLocalUI.enabled
	globalLocalUI.enabled = enabled
	globalLocalUI.mu.Unlock()
	t.Cleanup(func() {
		globalLocalUI.mu.Lock()
		globalLocalUI.enabled = prev
		globalLocalUI.mu.Unlock()
	})
}

func toolNames(t *testing.T, respBody []byte) map[string]bool {
	t.Helper()
	var env struct {
		Result struct {
			Tools []struct {
				Name string `json:"name"`
			} `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(respBody, &env); err != nil {
		t.Fatalf("decode response: %v (body: %s)", err, respBody)
	}
	out := map[string]bool{}
	for _, tool := range env.Result.Tools {
		out[tool.Name] = true
	}
	return out
}

// agentStyleToolsListResponse mimics what a software Agent backend
// (agent/internal/api/mcp.go's mcpToolCatalog) actually returns from
// tools/list -- no ui.parse, since the Agent has no detector of its own.
const agentStyleToolsListResponse = `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"screen.get_image","description":"...","inputSchema":{}},{"name":"keyboard.send","description":"...","inputSchema":{}},{"name":"mouse.action","description":"...","inputSchema":{}},{"name":"device.info","description":"...","inputSchema":{}}]}}`

// hardwareStyleToolsListResponse mimics the hardware KVM's own tools/list,
// which already includes its real ui.parse.
const hardwareStyleToolsListResponse = `{"jsonrpc":"2.0","id":1,"result":{"tools":[{"name":"screen.get","description":"...","inputSchema":{}},{"name":"ui.parse","description":"the real one","inputSchema":{}},{"name":"mouse.action","description":"...","inputSchema":{}}]}}`

const toolsListRequest = `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`

func TestInjectLocalUIParseTool_AddsToAgentCatalogWhenEnabled(t *testing.T) {
	withLocalUIEnabled(t, true)
	out := injectLocalUIParseTool([]byte(toolsListRequest), []byte(agentStyleToolsListResponse))
	names := toolNames(t, out)
	if !names["ui.parse"] {
		t.Fatalf("ui.parse not injected into agent-style tools/list response: %s", out)
	}
	for _, want := range []string{"screen.get_image", "keyboard.send", "mouse.action", "device.info"} {
		if !names[want] {
			t.Errorf("injection dropped existing tool %q: %s", want, out)
		}
	}
}

func TestInjectLocalUIParseTool_NoopWhenDisabled(t *testing.T) {
	withLocalUIEnabled(t, false)
	out := injectLocalUIParseTool([]byte(toolsListRequest), []byte(agentStyleToolsListResponse))
	if string(out) != agentStyleToolsListResponse {
		t.Fatalf("expected response untouched when local ui.parse is disabled, got: %s", out)
	}
}

func TestInjectLocalUIParseTool_NoDuplicateWhenBackendAlreadyHasIt(t *testing.T) {
	withLocalUIEnabled(t, true)
	out := injectLocalUIParseTool([]byte(toolsListRequest), []byte(hardwareStyleToolsListResponse))
	if string(out) != hardwareStyleToolsListResponse {
		t.Fatalf("expected response untouched when the backend (hardware KVM) already lists ui.parse, got: %s", out)
	}
}

func TestInjectLocalUIParseTool_NoopForNonToolsListMethods(t *testing.T) {
	withLocalUIEnabled(t, true)
	toolsCallReq := `{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"screen.get_image"}}`
	toolsCallResp := `{"jsonrpc":"2.0","id":1,"result":{"content":[]}}`
	out := injectLocalUIParseTool([]byte(toolsCallReq), []byte(toolsCallResp))
	if string(out) != toolsCallResp {
		t.Fatalf("expected non-tools/list responses untouched, got: %s", out)
	}
}

func TestInjectLocalUIParseTool_NoopOnMalformedResponse(t *testing.T) {
	withLocalUIEnabled(t, true)
	malformed := []byte(`not json`)
	out := injectLocalUIParseTool([]byte(toolsListRequest), malformed)
	if string(out) != string(malformed) {
		t.Fatalf("expected malformed response returned unchanged, got: %s", out)
	}
}

// TestInjectLocalUIParseTool_InjectedToolUsableRecipe pins the shape an MCP
// client actually needs: a name, a non-empty description mentioning the
// click_at recipe, and an object inputSchema with no required arguments
// (ui.parse takes none).
func TestInjectLocalUIParseTool_InjectedToolUsableRecipe(t *testing.T) {
	withLocalUIEnabled(t, true)
	out := injectLocalUIParseTool([]byte(toolsListRequest), []byte(agentStyleToolsListResponse))

	var env struct {
		Result struct {
			Tools []json.RawMessage `json:"tools"`
		} `json:"result"`
	}
	if err := json.Unmarshal(out, &env); err != nil {
		t.Fatalf("decode: %v", err)
	}
	var found map[string]any
	for _, raw := range env.Result.Tools {
		var tool map[string]any
		if err := json.Unmarshal(raw, &tool); err != nil {
			t.Fatalf("decode tool: %v", err)
		}
		if tool["name"] == "ui.parse" {
			found = tool
			break
		}
	}
	if found == nil {
		t.Fatal("ui.parse not found in injected tools")
	}
	desc, _ := found["description"].(string)
	if desc == "" {
		t.Error("injected ui.parse has empty description")
	}
	if _, ok := found["inputSchema"].(map[string]any); !ok {
		t.Error("injected ui.parse missing inputSchema object")
	}
}
