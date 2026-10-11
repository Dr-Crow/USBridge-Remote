package main

import (
	"context"
	"testing"
)

func TestWindowDiagnosticsOnlyFixedCategories(t *testing.T) {
	for _, tc := range []struct{ message, want string }{
		{"secret descriptor arbitrary path", "window_unavailable"},
		{"VersionUnavailable: WGL: arbitrary private driver details", "graphics_version_unavailable"},
		{"APIUnavailable: WGL: arbitrary details", "graphics_api_unavailable"},
		{"PlatformError: arbitrary details", "graphics_platform_error"},
		{"FormatUnavailable: arbitrary details", "graphics_format_unavailable"},
	} {
		d := new(windowDiagnostics)
		n, err := d.Write([]byte(tc.message))
		if n != len(tc.message) || err != nil || d.failure("window_unavailable") != tc.want {
			t.Fatal("diagnostic category mismatch")
		}
		if d.failure("window_identity_failed") != "window_identity_failed" {
			t.Fatal("diagnostic hid identity failure")
		}
		if !validWindowFailure(tc.want) {
			t.Fatal("invalid closed category")
		}
	}
	if validWindowFailure("arbitrary secret text") || validWindowFailure("") {
		t.Fatal("unknown category accepted")
	}
}

func TestWindowDiagnosticsBounded(t *testing.T) {
	d := new(windowDiagnostics)
	d.Write(make([]byte, 65537))
	d.Write([]byte("APIUnavailable"))
	if d.api || d.failure("window_unavailable") != "window_diagnostics_overflow" {
		t.Fatal("diagnostic budget not enforced")
	}
}

func TestWindowStartupCancellationRetiresBeforeLaterStages(t *testing.T) {
	stages := []string{"app", "window", "content", "show", "identity"}
	for cancelAt := -1; cancelAt < len(stages); cancelAt++ {
		ctx, cancel := context.WithCancel(context.Background())
		if cancelAt == -1 {
			cancel()
		}
		calls, retired := 0, 0
		steps := make([]windowStartupStep, 0, len(stages))
		for i, stage := range stages {
			steps = append(steps, windowStartupStep{stage, func() string {
				calls++
				if i == cancelAt {
					cancel()
				}
				return ""
			}})
		}
		code := runWindowStartup(ctx, steps, func() { retired++ })
		want := max(0, cancelAt)
		if code != "startup_cancelled_"+stages[want] || calls != cancelAt+1 || retired != 1 {
			t.Fatalf("cancellation at %d: stage=%q calls=%d retired=%d", cancelAt, code, calls, retired)
		}
		cancel()
	}
}

func TestWindowStartupSuccessKeepsOwnerAndFailureRetires(t *testing.T) {
	for _, stepFailure := range []string{"", "window_identity_failed", "private native text"} {
		calls, retired := 0, 0
		steps := []windowStartupStep{{"app", func() string { calls++; return stepFailure }}, {"window", func() string { calls++; return "" }}}
		code := runWindowStartup(context.Background(), steps, func() { retired++ })
		want := stepFailure
		if stepFailure == "private native text" {
			want = "window_unavailable"
		}
		if code != want || (stepFailure == "" && (calls != 2 || retired != 0)) || (stepFailure != "" && (calls != 1 || retired != 1)) {
			t.Fatal("startup lifecycle or closed failure changed")
		}
	}
	for _, step := range []windowStartupStep{{"unreviewed", func() string { t.Fatal("unreviewed stage ran"); return "" }}, {"show", nil}} {
		retired := false
		if runWindowStartup(context.Background(), []windowStartupStep{step}, func() { retired = true }) != "window_unavailable" || !retired {
			t.Fatal("invalid stage admitted")
		}
	}
}
