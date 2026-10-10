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
