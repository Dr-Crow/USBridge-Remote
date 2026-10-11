package main

import (
	"encoding/json"
	"fmt"
	"strings"
	"testing"
	"time"
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

func TestStartupStoppedClosedCategories(t *testing.T) {
	for _, reason := range []string{"completed", "failed"} {
		raw := []byte(fmt.Sprintf(`{"schema_version":1,"event":"stopped","session_id":"owned","reason":%q}`, reason))
		gotReason, gotCode, err := startupStopped(raw, "owned")
		if err != nil || gotReason != reason || gotCode != "" {
			t.Fatal("valid uncoded stopped event rejected")
		}
	}
	for _, code := range []string{"window_unavailable", "window_driver_unavailable", "window_identity_failed", "window_not_visible", "window_diagnostics_overflow", "graphics_api_unavailable", "graphics_version_unavailable", "graphics_platform_error", "graphics_format_unavailable"} {
		raw := []byte(fmt.Sprintf(`{"schema_version":1,"event":"stopped","session_id":"owned","reason":"failed","failure_code":%q}`, code))
		gotReason, gotCode, err := startupStopped(raw, "owned")
		if err != nil || gotReason != "failed" || gotCode != code {
			t.Fatal("valid coded stopped event rejected")
		}
	}
	base := `{"schema_version":1,"event":"stopped","session_id":"owned","reason":"failed"}`
	for _, raw := range []string{
		strings.Replace(base, `"failed"`, `"private driver error"`, 1),
		strings.Replace(base, `"failed"`, `null`, 1),
		strings.Replace(base, `"reason"`, `"REASON"`, 1),
		strings.Replace(base, `"owned"`, `"other"`, 1),
		strings.Replace(base, `"stopped"`, `"first_frame"`, 1),
		strings.Replace(base, `:1`, `:2`, 1),
		strings.Replace(base, `,"reason":"failed"`, ``, 1),
		strings.TrimSuffix(base, "}") + `,"reason":"completed"}`,
		strings.TrimSuffix(base, "}") + `,"failure_code":"private driver error"}`,
		strings.TrimSuffix(base, "}") + `,"failure_code":""}`,
		strings.TrimSuffix(base, "}") + `,"failure_code":null}`,
		strings.TrimSuffix(strings.Replace(base, `"failed"`, `"completed"`, 1), "}") + `,"failure_code":"window_unavailable"}`,
		strings.TrimSuffix(base, "}") + `,"stderr":"private driver error"}`,
		base + `{}`,
	} {
		reason, code, err := startupStopped([]byte(raw), "owned")
		if err == nil || reason != "" || code != "" || err.Error() != "invalid_startup_terminal" {
			t.Fatal("invalid stopped event escaped the closed decoder")
		}
	}
}

func TestStartupMillisBounded(t *testing.T) {
	for _, tc := range []struct {
		elapsed time.Duration
		want    uint32
	}{
		{-time.Second, 0},
		{0, 0},
		{999 * time.Microsecond, 0},
		{1250 * time.Millisecond, 1250},
		{5 * time.Second, 5000},
		{15 * time.Second, 15000},
		{60 * time.Second, 60000},
		{24 * time.Hour, 60000},
		{time.Duration(1<<63 - 1), 60000},
	} {
		if got := startupMillis(tc.elapsed); got == nil || *got != tc.want {
			t.Fatalf("elapsed %s did not produce bounded milliseconds %d", tc.elapsed, tc.want)
		}
	}
}

func TestStartupDiagnosticsDistinguishMissingAndObservedZero(t *testing.T) {
	alive, exitCode := false, uint32(0)
	d := windowStartupDiagnostics{
		DescriptorWrittenMillis: startupMillis(0),
		ChildAliveBeforeEOF:     &alive,
		ExitCode:                &exitCode,
	}
	raw, err := json.Marshal(d)
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]json.RawMessage
	if err := json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, field := range []string{"first_owned_window_ms", "actual_owned_viewer_window_ms", "observation_ended_ms", "eof_requested_ms", "natural_join_ms", "terminal_reason_before_eof", "terminal_reason_after_eof"} {
		if _, ok := fields[field]; ok {
			t.Fatal("unobserved milestone was serialized as observed")
		}
	}
	for field, want := range map[string]string{
		"descriptor_written_ms":               "0",
		"child_alive_before_eof":              "false",
		"viewer_exit_code_after_natural_join": "0",
		"observation_deadline_reached":        "false",
		"terminal_observed_before_eof":        "false",
		"empty_stderr_verified":               "false",
	} {
		if string(fields[field]) != want {
			t.Fatalf("observed diagnostic %s missing or incorrect", field)
		}
	}
}
