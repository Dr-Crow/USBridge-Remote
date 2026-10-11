package main

import (
	"bytes"
	"context"
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
	case "startup_cancelled_app", "startup_cancelled_window", "startup_cancelled_content", "startup_cancelled_show", "startup_cancelled_identity",
		"window_unavailable", "window_driver_unavailable", "window_identity_failed", "window_not_visible",
		"window_diagnostics_overflow", "graphics_api_unavailable", "graphics_version_unavailable", "graphics_platform_error", "graphics_format_unavailable":
		return true
	}
	return false
}

// The operations are synchronous main-thread Fyne calls. We cannot interrupt a
// native call safely, but must not advance startup once it returns to an ended
// lease. Fixed stages reveal no descriptor, path, native error or key.
type windowStartupStep struct {
	stage string
	run   func() string
}

func runWindowStartup(ctx context.Context, steps []windowStartupStep, retire func()) (failure string) {
	defer func() {
		if failure != "" {
			retire()
		}
	}()
	for _, step := range steps {
		code := "startup_cancelled_" + step.stage
		if !validWindowFailure(code) || step.run == nil {
			return "window_unavailable"
		}
		if ctx.Err() != nil {
			return code
		}
		if code := step.run(); code != "" {
			if !validWindowFailure(code) {
				return "window_unavailable"
			}
			return code
		}
		if ctx.Err() != nil {
			return code
		}
	}
	return ""
}
