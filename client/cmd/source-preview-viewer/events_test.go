package main

import (
	"bytes"
	"encoding/json"
	"strings"
	"sync"
	"testing"
)

func TestEventsOrderAndSingleFrame(t *testing.T) {
	for _, frameFirst := range []bool{false, true} {
		var out bytes.Buffer
		e := &events{out: json.NewEncoder(&out), sessionID: "session_0123456789"}
		if frameFirst {
			e.displayed()
		}
		e.connected()
		e.connected()
		var wg sync.WaitGroup
		for range 32 {
			wg.Go(e.displayed)
		}
		wg.Wait()
		e.stopped("closed")
		lines := strings.Split(strings.TrimSpace(out.String()), "\n")
		if len(lines) != 3 {
			t.Fatalf("events: %s", out.String())
		}
		for i, want := range []string{"ready", "first_frame", "stopped"} {
			var v map[string]any
			if err := json.Unmarshal([]byte(lines[i]), &v); err != nil {
				t.Fatal(err)
			}
			if v["schema_version"] != float64(1) || v["event"] != want || v["session_id"] != "session_0123456789" {
				t.Fatalf("bad event: %v", v)
			}
			if i == 2 {
				if v["reason"] != "completed" {
					t.Fatal("bad reason")
				}
			} else if _, ok := v["reason"]; ok {
				t.Fatal("reason before stop")
			}
		}
	}
}
func TestEventsDoNotExportFailureText(t *testing.T) {
	var out bytes.Buffer
	e := &events{out: json.NewEncoder(&out), sessionID: "session_0123456789"}
	e.stopped("renderer_error")
	if strings.Contains(out.String(), "renderer_error") || !strings.Contains(out.String(), `"reason":"failed"`) {
		t.Fatal("internal reason leaked")
	}
}

func TestNoEventsAfterTerminal(t *testing.T) {
	var out bytes.Buffer
	e := &events{out: json.NewEncoder(&out), sessionID: "session_0123456789"}
	e.connected()
	e.stopped("closed")
	e.displayed()
	e.connected()
	e.stopped("renderer_error")
	if len(strings.Split(strings.TrimSpace(out.String()), "\n")) != 2 {
		t.Fatalf("events after terminal: %s", out.String())
	}
}

func TestWindowStartupFailureOnlyExportsClosedCode(t *testing.T) {
	for _, code := range []string{"graphics_api_unavailable", "secret native error"} {
		var out bytes.Buffer
		e := &events{out: json.NewEncoder(&out), sessionID: "session_0123456789"}
		e.startupFailed(code)
		e.connected()
		e.displayed()
		e.stopped("closed")
		var value map[string]any
		if json.Unmarshal(out.Bytes(), &value) != nil || len(value) != 5 || value["reason"] != "failed" || value["event"] != "stopped" {
			t.Fatal("invalid startup event")
		}
		want := code
		if code == "secret native error" {
			want = "window_unavailable"
		}
		if value["failure_code"] != want || strings.Contains(out.String(), "secret") {
			t.Fatal("native text escaped")
		}
	}
}
