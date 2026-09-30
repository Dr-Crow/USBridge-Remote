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

// awdlPollInterval is how often awdlWatchdog re-checks session state and
// re-asserts awdl0 down. macOS itself brings awdl0 back up on its own
// after a while even while held down (confirmed live) -- a single toggle
// at session start isn't enough, so this has to keep re-issuing `ifconfig
// awdl0 down` for as long as the session stays active, the same way a
// manual `while true; do sudo ifconfig awdl0 down; sleep 1; done` loop
// would. 1s matches that same proven-good cadence; the call itself is a
// cheap, idempotent local ifconfig invocation (a few ms), so polling this
// often is not meaningful overhead.
const awdlPollInterval = 1 * time.Second

// awdlWatchdog polls the running streamer's session state (see
// streamhost.Backend.SessionActive) every awdlPollInterval. While a
// session is active it unconditionally re-asserts awdl0 down on every
// tick (see awdlPollInterval's doc comment for why "once at session
// start" isn't enough); once the session ends it brings awdl0 back up
// exactly once. A no-op loop (returns immediately) unless
// AWDLDisableDuringStreamingSupported is true, so it's always safe for
// Run() to start this unconditionally on every platform.
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
			if !active {
				restore()
				continue
			}
			if err := netutil.SetAWDLDown(true); err != nil {
				log.Printf("[app] AWDL re-assert (down=true) failed: %v", err)
				continue
			}
			down = true
		}
	}
}
