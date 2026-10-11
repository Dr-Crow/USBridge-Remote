package main

import (
	"encoding/json"
	"os"
	"strings"
	"time"
)

// These are elapsed monotonic-clock measurements from the viewer launch request,
// never wall-clock times. Missing milestones were not observed. Values saturate
// at one minute so even an unexpectedly stalled run has bounded diagnostics.
type windowStartupDiagnostics struct {
	DescriptorWrittenMillis      *uint32 `json:"descriptor_written_ms,omitempty"`
	FirstOwnedWindowMillis       *uint32 `json:"first_owned_window_ms,omitempty"`
	FirstVisibleWindowMillis     *uint32 `json:"first_visible_window_ms,omitempty"`
	FirstTitleMatchMillis        *uint32 `json:"first_title_match_ms,omitempty"`
	WindowVerifiedMillis         *uint32 `json:"actual_owned_viewer_window_ms,omitempty"`
	FirstChildExitObservedMillis *uint32 `json:"first_child_exit_observed_ms,omitempty"`
	ObservationEndedMillis       *uint32 `json:"observation_ended_ms,omitempty"`
	ObservationEnd               string  `json:"observation_end,omitempty"`
	ObservationDeadlineReached   bool    `json:"observation_deadline_reached"`
	EOFRequestedMillis           *uint32 `json:"eof_requested_ms,omitempty"`
	ChildAliveBeforeEOF          *bool   `json:"child_alive_before_eof,omitempty"`
	TerminalBeforeEOF            bool    `json:"terminal_observed_before_eof"`
	TerminalReasonBeforeEOF      string  `json:"terminal_reason_before_eof,omitempty"`
	TerminalReasonAfterEOF       string  `json:"terminal_reason_after_eof,omitempty"`
	NaturalJoinMillis            *uint32 `json:"natural_join_ms,omitempty"`
	ExitCode                     *uint32 `json:"viewer_exit_code_after_natural_join,omitempty"`
	EmptyStderrVerified          bool    `json:"empty_stderr_verified"`
}

func startupMillis(elapsed time.Duration) *uint32 {
	value := uint32(max(int64(0), min(elapsed.Milliseconds(), int64(60000))))
	return &value
}

type windowStartupReceipt struct {
	Diagnostics       *windowStartupDiagnostics       `json:"startup_diagnostics,omitempty"`
	VerifiedOSModules map[string]graphicsOSInspection `json:"verified_os_graphics_modules,omitempty"`
	Schema            int                             `json:"schema_version"`
	Commit            string                          `json:"commit"`
	ViewerSHA         string                          `json:"viewer_sha256"`
	Passed            bool                            `json:"passed"`
	Failure           string                          `json:"failure_code,omitempty"`
	ViewerFailure     string                          `json:"viewer_startup_failure_code,omitempty"`
	OwnedWindows      int                             `json:"owned_top_level_windows_max"`
	VisibleWindows    int                             `json:"owned_visible_windows_max"`
	TitleMatches      int                             `json:"owned_title_matches_max"`
	WindowVerified    bool                            `json:"actual_owned_viewer_window_verified"`
	NaturalCleanup    bool                            `json:"natural_cleanup"`
	JobEmpty          bool                            `json:"job_empty_before_safety_close"`
	SafetyKill        bool                            `json:"safety_job_kill_used"`
	ActualMedia       bool                            `json:"actual_media_tested"`
	Pixels            bool                            `json:"actual_window_pixels_tested"`
	DesktopCapture    bool                            `json:"desktop_capture_tested"`
	GraphicsModules   map[string]string               `json:"viewer_graphics_modules_sha256,omitempty"`
	GraphicsRejection *graphicsModuleRejection        `json:"graphics_module_rejection,omitempty"`
}

func runWindowStartup(c config) {
	r := windowStartupReceipt{Schema: 1}
	if commitPattern.MatchString(c.Commit) {
		r.Commit = c.Commit
	}
	if hashPattern.MatchString(c.ViewerSHA) {
		r.ViewerSHA = c.ViewerSHA
	}
	err := validateWindowStartup(c)
	if err == nil {
		err = executeWindowStartup(c, &r)
	}
	if err != nil {
		r.Failure = err.Error()
		r.GraphicsRejection = graphicsRejection(err)
	} else {
		r.Passed = true
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil || writeReceipt(c.Output, append(raw, '\n')) != nil {
		os.Exit(2)
	}
	if !r.Passed {
		os.Exit(1)
	}
}

func validateWindowStartup(c config) error {
	if err := validateGraphicsConfig(c); err != nil {
		return err
	}
	if !commitPattern.MatchString(c.Commit) || !hashPattern.MatchString(c.ViewerSHA) {
		return failure("invalid_startup_identity")
	}
	if c.Agent != "" || c.AgentSHA != "" || c.Fixture != "" || c.Components != "" || c.SourceSHA != "" || c.ManifestSHA != "" || c.FixtureSHA != "" || c.FixtureManifestSHA != "" {
		return failure("unexpected_startup_component")
	}
	for _, p := range []string{c.Viewer, c.Work, c.Output} {
		if !drivePath.MatchString(p) || strings.ContainsAny(p, "\x00\r\n") || strings.Contains(p[2:], ":") {
			return failure("invalid_local_path")
		}
	}
	if !strings.HasSuffix(strings.ToLower(c.Viewer), ".exe") {
		return failure("invalid_executable_path")
	}
	return nil
}

func startupTerminal(raw []byte, id string) (string, error) {
	var e struct {
		Schema  int    `json:"schema_version"`
		Event   string `json:"event"`
		Session string `json:"session_id"`
		Reason  string `json:"reason"`
		Failure string `json:"failure_code"`
	}
	if exactJSON(raw, &e, "schema_version", "event", "session_id", "reason", "failure_code") != nil || e.Schema != 1 || e.Event != "stopped" || e.Session != id || e.Reason != "failed" {
		return "", failure("invalid_startup_terminal")
	}
	switch e.Failure {
	case "window_unavailable", "window_driver_unavailable", "window_identity_failed", "window_not_visible", "window_diagnostics_overflow", "graphics_api_unavailable", "graphics_version_unavailable", "graphics_platform_error", "graphics_format_unavailable":
		return e.Failure, nil
	}
	return "", failure("invalid_startup_terminal")
}

// A stopped event has either the original exact schema or the existing closed
// startup-failure schema. No child text is copied into the receipt.
func startupStopped(raw []byte, id string) (reason, code string, err error) {
	for _, reason := range []string{"completed", "failed"} {
		if parseViewer(raw, id, "stopped", reason) == nil {
			return reason, "", nil
		}
	}
	code, err = startupTerminal(raw, id)
	if err != nil {
		return "", "", err
	}
	return "failed", code, nil
}
