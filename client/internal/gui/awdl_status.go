package gui

import (
	"time"

	"usbridge-client/internal/gui/assets"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/netutil"

	"fyne.io/fyne/v2"
	"github.com/sirupsen/logrus"
)

// awdlDisablePrefKey persists the "disable AWDL while streaming" toggle via
// Fyne's own app.Preferences() -- same lightweight per-key store
// video_preferences.go's loadVideoPreferences/saveVideoPreferences already
// use, rather than the heavier viper-backed AppConfig (which nothing in
// this app currently writes back to disk from the running UI).
const awdlDisablePrefKey = "awdl_disable_during_streaming"

// awdlEnabled reports the persisted setting -- off by default (opt-in):
// suppressing AWDL affects AirDrop/Handoff/Sidecar system-wide, not
// something to switch on silently.
func (mw *MainWindow) awdlEnabled() bool {
	if mw.app == nil {
		return false
	}
	return mw.app.Preferences().BoolWithFallback(awdlDisablePrefKey, false)
}

// awdlGranted reports whether the one-time sudoers permission is already
// in effect. Shared with USBridge-Remote/agent's identical rule path (see
// netutil/awdl_darwin.go's doc comment) -- if the agent already granted
// it on this machine, the client needs no separate prompt.
func (mw *MainWindow) awdlGranted() bool {
	return netutil.AWDLSudoRuleInstalled()
}

// setAWDLEnabled persists the toggle, installing the sudoers rule (one
// admin-password prompt via osascript) the first time it's turned on.
// Turning it off restores awdl0 immediately if this process currently
// holds it down. Callers run this off the UI goroutine -- EnsureAWDLSudoRule
// blocks on the admin prompt.
func (mw *MainWindow) setAWDLEnabled(enabled bool) error {
	if enabled {
		if err := netutil.EnsureAWDLSudoRule(); err != nil {
			return err
		}
	}
	if mw.app != nil {
		mw.app.Preferences().SetBool(awdlDisablePrefKey, enabled)
	}
	if !enabled && mw.awdlSuppressing {
		if err := netutil.SetAWDLDown(false); err != nil {
			logrus.Warnf("[awdl] restoring AWDL after disabling the setting: %v", err)
		}
		mw.awdlSuppressing = false
	}
	return nil
}

// awdlPollInterval matches USBridge-Remote/agent's own awdlWatchdog: macOS
// brings awdl0 back up on its own after a while even while held down
// (confirmed live), so a single toggle at stream start isn't enough --
// awdlWatchdog below keeps re-asserting it every second for as long as
// the stream stays active, the same way a manual `while true; do sudo
// ifconfig awdl0 down; sleep 1; done` loop would. The call itself is a
// cheap, idempotent local ifconfig invocation, so this cadence isn't
// meaningful overhead.
const awdlPollInterval = 1 * time.Second

// syncAWDLStreamingState is called both from updateStatus() (for a fast
// reaction right at the real start/stop transition, via the video
// widget's own callback -- see main_window_connection.go) and from
// awdlWatchdog's ticker (for continuous re-assertion in between). While
// streaming+enabled it unconditionally re-asserts awdl0 down every call;
// once not streaming (or the setting is off) it brings awdl0 back up
// exactly once.
func (mw *MainWindow) syncAWDLStreamingState(isStreaming bool) {
	if !awdlSupported() || !mw.awdlEnabled() || !isStreaming {
		if mw.awdlSuppressing {
			if err := netutil.SetAWDLDown(false); err != nil {
				logrus.Warnf("[awdl] restoring AWDL: %v", err)
			} else {
				mw.awdlSuppressing = false
			}
		}
		return
	}
	if err := netutil.SetAWDLDown(true); err != nil {
		logrus.Warnf("[awdl] re-assert (down=true) failed: %v", err)
		return
	}
	mw.awdlSuppressing = true
}

// startAWDLWatchdog runs for the lifetime of the window (stopped via
// mw.isClosing, same pattern as startDeepLinkMonitoring), continuously
// re-asserting awdl0 down while mw.isStreaming and the setting is on --
// see syncAWDLStreamingState's and awdlPollInterval's doc comments for
// why a one-shot toggle at stream start isn't enough on macOS. A no-op
// loop (returns immediately) everywhere awdlSupported() is false.
func (mw *MainWindow) startAWDLWatchdog() {
	if !awdlSupported() {
		return
	}
	go func() {
		ticker := time.NewTicker(awdlPollInterval)
		defer ticker.Stop()
		for range ticker.C {
			if mw.isClosing.Load() {
				return
			}
			mw.syncAWDLStreamingState(mw.isStreaming)
		}
	}()
}

// refreshAWDLUI syncs both the footer icon and the header grant button to
// current state. Safe to call whenever (nil-checked), from updateStatus's
// per-tick refresh and right after any toggle/grant action completes.
func (mw *MainWindow) refreshAWDLUI() {
	if mw.awdlIcon != nil {
		if !awdlSupported() {
			mw.awdlIcon.Hide()
		} else {
			mw.awdlIcon.Show()
			switch {
			case mw.awdlSuppressing:
				mw.awdlIcon.SetIcon(assets.AWDLIconStatusBar)
			case mw.awdlEnabled():
				mw.awdlIcon.SetIcon(assets.AWDLIconActive)
			default:
				mw.awdlIcon.SetIcon(assets.AWDLIcon)
			}
			mw.awdlIcon.Refresh()
		}
	}
	if mw.awdlGrantBtn != nil {
		if awdlSupported() && !mw.awdlGranted() {
			mw.awdlGrantBtn.Show()
		} else {
			mw.awdlGrantBtn.Hide()
		}
	}
}

// showAWDLMenu is the footer icon's click target: a single toggle item,
// same shape as clipboardAutoSyncMenuItem/showClipboardMenu.
func (mw *MainWindow) showAWDLMenu() {
	if mw.awdlIcon == nil {
		return
	}
	enabled := mw.awdlEnabled()
	items := []view.StyledMenuItem{
		{
			Label:          i18n.Current.AWDLDisableDuringStreaming,
			SecondaryLabel: i18n.Current.AWDLDisableDuringStreamingHint,
			Selected:       enabled,
			OnTap: func() {
				next := !enabled
				go func() {
					err := mw.setAWDLEnabled(next)
					fyne.Do(func() {
						if err != nil {
							view.ShowErrorDialog(err, mw.window)
							return
						}
						mw.refreshAWDLUI()
					})
				}()
			},
		},
	}
	view.ShowStyledMenuTealAbove(mw.awdlIcon, items)
}

// handleAWDLGrantTapped is the prominent header button's click target
// (main_window_layout.go's rightGroup) -- a shortcut straight to "turn the
// setting on", since that button's whole purpose is inviting the user to
// grant the one-time permission, more visible than the footer icon's menu.
func (mw *MainWindow) handleAWDLGrantTapped() {
	if mw.awdlGrantBtn == nil {
		return
	}
	mw.awdlGrantBtn.Disable()
	go func() {
		err := mw.setAWDLEnabled(true)
		fyne.Do(func() {
			if mw.awdlGrantBtn != nil {
				mw.awdlGrantBtn.Enable()
			}
			if err != nil {
				view.ShowErrorDialog(err, mw.window)
				return
			}
			mw.refreshAWDLUI()
		})
	}()
}
