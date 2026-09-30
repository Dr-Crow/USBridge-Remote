package app

import (
	"context"
	"log"
	"runtime"
	"time"

	"usbridge_agent/internal/netutil"
)

// AWDLDisableDuringStreamingSupported reports whether the "disable AWDL
// during streaming" setting should be offered at all -- macOS only, since
// AWDL (Apple Wireless Direct Link, the mesh protocol behind
// AirDrop/Handoff/Sidecar) doesn't exist elsewhere. See
// internal/netutil/awdl_darwin.go for why disabling it helps a concurrent
// Wi-Fi stream.
func (a *App) AWDLDisableDuringStreamingSupported() bool {
	return runtime.GOOS == "darwin"
}

// AWDLDisableDuringStreamingEnabled returns the persisted setting.
func (a *App) AWDLDisableDuringStreamingEnabled() bool {
	return a.cfg.DisableAWDLDuringStreaming
}

// SetAWDLDisableDuringStreaming persists the setting, installing the
// sudoers.d NOPASSWD rule (one admin-password prompt) the first time it's
// turned on -- see netutil.EnsureAWDLSudoRule. Turning it off immediately
// restores awdl0 if awdlWatchdog had it down; turning it on doesn't force
// it down right away -- the watchdog picks that up within its own poll
// interval once a session is actually active.
func (a *App) SetAWDLDisableDuringStreaming(enabled bool) error {
	if enabled {
		if err := netutil.EnsureAWDLSudoRule(); err != nil {
			return err
		}
	}
	next := a.cfg
	next.DisableAWDLDuringStreaming = enabled
	if err := a.SaveConfig(next); err != nil {
		return err
	}
	if !enabled {
		if err := netutil.SetAWDLDown(false); err != nil {
			log.Printf("[app] restoring AWDL after disabling the setting: %v", err)
		}
	}
	return nil
}

// AWDLSudoersPreview returns the exact sudoers.d rule that
// SetAWDLDisableDuringStreaming(true) would install, for the UI's "show
// what this is about to do" info button -- see
// netutil.AWDLSudoersPreview.
func (a *App) AWDLSudoersPreview() string {
	return netutil.AWDLSudoersPreview()
}

// awdlPollInterval is how often awdlWatchdog checks session state. Short
// enough that AWDL comes back up within a couple seconds of a client
// disconnecting (so AirDrop/Handoff aren't left off any longer than
// necessary), long enough not to matter as overhead.
const awdlPollInterval = 2 * time.Second

// awdlWatchdog polls the running streamer's session state (see
// streamhost.Backend.SessionActive) and toggles awdl0 down for the
// duration of an active streaming session, back up otherwise. A no-op loop
// (returns immediately) unless AWDLDisableDuringStreamingSupported is
// true, so it's always safe for Run() to start this unconditionally on
// every platform.
func (a *App) awdlWatchdog(ctx context.Context) {
	if !a.AWDLDisableDuringStreamingSupported() {
		return
	}
	ticker := time.NewTicker(awdlPollInterval)
	defer ticker.Stop()

	down := false
	restore := func() {
		if !down {
			return
		}
		if err := netutil.SetAWDLDown(false); err != nil {
			log.Printf("[app] restoring AWDL: %v", err)
			return
		}
		down = false
	}
	for {
		select {
		case <-ctx.Done():
			restore()
			return
		case <-ticker.C:
			if !a.AWDLDisableDuringStreamingEnabled() {
				restore()
				continue
			}
			a.streamMu.Lock()
			stream := a.stream
			a.streamMu.Unlock()
			active := stream != nil && stream.SessionActive()
			if active == down {
				continue
			}
			if err := netutil.SetAWDLDown(active); err != nil {
				log.Printf("[app] AWDL toggle (down=%v) failed: %v", active, err)
				continue
			}
			down = active
		}
	}
}
