package main

import (
	"encoding/json"
	"os"
	"strings"
)

type windowStartupReceipt struct {
	Schema            int                      `json:"schema_version"`
	Commit            string                   `json:"commit"`
	ViewerSHA         string                   `json:"viewer_sha256"`
	Passed            bool                     `json:"passed"`
	Failure           string                   `json:"failure_code,omitempty"`
	ViewerFailure     string                   `json:"viewer_startup_failure_code,omitempty"`
	OwnedWindows      int                      `json:"owned_top_level_windows_max"`
	VisibleWindows    int                      `json:"owned_visible_windows_max"`
	TitleMatches      int                      `json:"owned_title_matches_max"`
	WindowVerified    bool                     `json:"actual_owned_viewer_window_verified"`
	NaturalCleanup    bool                     `json:"natural_cleanup"`
	JobEmpty          bool                     `json:"job_empty_before_safety_close"`
	SafetyKill        bool                     `json:"safety_job_kill_used"`
	ActualMedia       bool                     `json:"actual_media_tested"`
	Pixels            bool                     `json:"actual_window_pixels_tested"`
	DesktopCapture    bool                     `json:"desktop_capture_tested"`
	GraphicsModules   map[string]string        `json:"viewer_graphics_modules_sha256,omitempty"`
	GraphicsRejection *graphicsModuleRejection `json:"graphics_module_rejection,omitempty"`
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
