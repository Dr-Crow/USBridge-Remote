package main

import "testing"

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
