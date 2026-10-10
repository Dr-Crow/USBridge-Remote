package main

import (
	"fmt"
	"strings"
	"testing"
)

func TestStartupTerminalClosedCategories(t *testing.T) {
	for _, code := range []string{"window_unavailable", "window_driver_unavailable", "window_identity_failed", "window_not_visible", "window_diagnostics_overflow", "graphics_api_unavailable", "graphics_version_unavailable", "graphics_platform_error", "graphics_format_unavailable"} {
		raw := []byte(fmt.Sprintf(`{"schema_version":1,"event":"stopped","session_id":"owned","reason":"failed","failure_code":%q}`, code))
		if got, err := startupTerminal(raw, "owned"); err != nil || got != code {
			t.Fatal("valid category rejected")
		}
		for _, mutation := range []string{strings.Replace(string(raw), `"failed"`, `"completed"`, 1), strings.Replace(string(raw), `"failure_code"`, `"FAILURE_CODE"`, 1), strings.Replace(string(raw), code, "private driver error", 1), strings.Replace(string(raw), `"owned"`, `"other"`, 1)} {
			if _, err := startupTerminal([]byte(mutation), "owned"); err == nil {
				t.Fatal("invalid startup event accepted")
			}
		}
	}
}

func TestStartupModeRejectsComponentsAndInvalidIdentity(t *testing.T) {
	c := config{Viewer: `C:\owned\viewer.exe`, ViewerSHA: strings.Repeat("a", 64), Commit: strings.Repeat("b", 40), Work: `C:\owned\work`, Output: `C:\owned\receipt.json`}
	if err := validateWindowStartup(c); err != nil {
		t.Fatal(err)
	}
	c.Agent = `C:\owned\agent.exe`
	if validateWindowStartup(c) == nil {
		t.Fatal("component execution accepted")
	}
	c.Agent = ""
	c.ViewerSHA = "bad"
	if validateWindowStartup(c) == nil {
		t.Fatal("unverified viewer accepted")
	}
}
