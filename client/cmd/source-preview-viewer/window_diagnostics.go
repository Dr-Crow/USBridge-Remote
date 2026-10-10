package main

import (
	"bytes"
	"log"
	"sync"
)

// Only fixed categories survive this short, pre-network startup phase. Native
// diagnostics, paths and driver strings are never retained or forwarded.
type windowDiagnostics struct {
	mu                             sync.Mutex
	bytes                          int
	api, version, platform, format bool
}

func (d *windowDiagnostics) Write(p []byte) (int, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.bytes = min(65537, d.bytes+len(p))
	if d.bytes <= 65536 {
		d.api = d.api || bytes.Contains(p, []byte("APIUnavailable"))
		d.version = d.version || bytes.Contains(p, []byte("VersionUnavailable"))
		d.platform = d.platform || bytes.Contains(p, []byte("PlatformError"))
		d.format = d.format || bytes.Contains(p, []byte("FormatUnavailable"))
	}
	return len(p), nil
}

func beginWindowDiagnostics() (*windowDiagnostics, func()) {
	d := new(windowDiagnostics)
	previous := log.Writer()
	log.SetOutput(d)
	return d, func() { log.SetOutput(previous) }
}

func (d *windowDiagnostics) failure(fallback string) string {
	d.mu.Lock()
	defer d.mu.Unlock()
	if fallback != "window_unavailable" {
		return fallback
	}
	if d.bytes > 65536 {
		return "window_diagnostics_overflow"
	}
	if d.api {
		return "graphics_api_unavailable"
	}
	if d.version {
		return "graphics_version_unavailable"
	}
	if d.platform {
		return "graphics_platform_error"
	}
	if d.format {
		return "graphics_format_unavailable"
	}
	return fallback
}

func validWindowFailure(code string) bool {
	switch code {
	case "window_unavailable", "window_driver_unavailable", "window_identity_failed", "window_not_visible",
		"window_diagnostics_overflow", "graphics_api_unavailable", "graphics_version_unavailable", "graphics_platform_error", "graphics_format_unavailable":
		return true
	}
	return false
}
