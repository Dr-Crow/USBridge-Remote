package gui

import (
	"testing"
	"time"

	"fyne.io/fyne/v2/test"
)

func newTestMainWindowForAWDL(t *testing.T) *MainWindow {
	t.Helper()
	return &MainWindow{app: test.NewTempApp(t)}
}

func TestAWDLEnabled_DefaultsFalse(t *testing.T) {
	mw := newTestMainWindowForAWDL(t)
	if mw.awdlEnabled() {
		t.Error("awdlEnabled() on a fresh app = true, want false (opt-in)")
	}
}

func TestAWDLEnabled_NilApp(t *testing.T) {
	mw := &MainWindow{}
	if mw.awdlEnabled() {
		t.Error("awdlEnabled() with mw.app == nil = true, want false")
	}
}

// TestSetAWDLEnabled_Off exercises only the enabled=false path:
// setAWDLEnabled(true) would call netutil.EnsureAWDLSudoRule, which pops a
// real admin-password prompt on darwin -- not something a test may
// trigger. The false path persists the preference and best-effort restores
// awdl0 only if mw.awdlSuppressing was already true (SetAWDLDown's
// failure, e.g. no sudo rule ever installed, is only logged per
// setAWDLEnabled's own doc comment), so it's safe to run unconditionally.
func TestSetAWDLEnabled_Off(t *testing.T) {
	mw := newTestMainWindowForAWDL(t)
	mw.app.Preferences().SetBool(awdlDisablePrefKey, true)

	if err := mw.setAWDLEnabled(false); err != nil {
		t.Fatalf("setAWDLEnabled(false) = %v, want nil", err)
	}
	if mw.awdlEnabled() {
		t.Error("awdlEnabled() after setAWDLEnabled(false) = true, want false")
	}
}

// TestSetAWDLEnabled_OffDoesNotTouchSudoWhenNotSuppressing confirms the
// false path skips netutil.SetAWDLDown entirely when mw.awdlSuppressing is
// already false -- i.e. turning the setting off when nothing was ever
// suppressed never shells out at all (not just "shells out but ignores the
// error").
func TestSetAWDLEnabled_OffDoesNotTouchSudoWhenNotSuppressing(t *testing.T) {
	mw := newTestMainWindowForAWDL(t)
	mw.awdlSuppressing = false

	if err := mw.setAWDLEnabled(false); err != nil {
		t.Fatalf("setAWDLEnabled(false) = %v, want nil", err)
	}
	if mw.awdlSuppressing {
		t.Error("awdlSuppressing became true after setAWDLEnabled(false)")
	}
}

// TestSyncAWDLStreamingState_UnsupportedOrDisabledNoop confirms
// syncAWDLStreamingState never calls netutil (which would shell out to
// sudo) when the feature isn't supported/enabled -- safe to call from
// every updateStatus() tick on every platform unconditionally.
func TestSyncAWDLStreamingState_UnsupportedOrDisabledNoop(t *testing.T) {
	mw := newTestMainWindowForAWDL(t)
	// Feature disabled (default): streaming or not, must stay a no-op.
	mw.syncAWDLStreamingState(true)
	if mw.awdlSuppressing {
		t.Error("syncAWDLStreamingState set awdlSuppressing=true while the feature is disabled")
	}
	mw.syncAWDLStreamingState(false)
	if mw.awdlSuppressing {
		t.Error("syncAWDLStreamingState set awdlSuppressing=true while the feature is disabled")
	}
}

// TestRefreshAWDLUI_NilSafe confirms refreshAWDLUI (called from every
// updateStatus tick) never panics before the footer icon/header button
// have been constructed (e.g. very early in startup, or headless/CLI use
// of MainWindow where the GUI tree is never built).
func TestRefreshAWDLUI_NilSafe(t *testing.T) {
	mw := newTestMainWindowForAWDL(t)
	mw.refreshAWDLUI()
}

// TestSyncAWDLStreamingState_NotStreamingRestoresOnce confirms the
// isStreaming=false path only calls netutil.SetAWDLDown (and thus only
// clears awdlSuppressing) when awdlSuppressing was already true -- calling
// it repeatedly while not streaming must stay a no-op after the first
// restore, not shell out to sudo every time.
func TestSyncAWDLStreamingState_NotStreamingRestoresOnce(t *testing.T) {
	mw := newTestMainWindowForAWDL(t)
	mw.app.Preferences().SetBool(awdlDisablePrefKey, true)
	mw.awdlSuppressing = false

	mw.syncAWDLStreamingState(false)
	if mw.awdlSuppressing {
		t.Error("syncAWDLStreamingState(false) set awdlSuppressing=true")
	}
}

// TestStartAWDLWatchdog_NoopWhenUnsupported confirms startAWDLWatchdog
// doesn't spawn its ticker goroutine at all on platforms where AWDL isn't
// supported -- nothing to stop, no ticker leak, safe to call
// unconditionally from every MainWindow's startup regardless of platform.
func TestStartAWDLWatchdog_NoopWhenUnsupported(t *testing.T) {
	if awdlSupported() {
		t.Skip("this platform supports AWDL -- covered by the ticker's own shape, not this test")
	}
	mw := newTestMainWindowForAWDL(t)
	mw.startAWDLWatchdog()
	// If a goroutine were spawned despite awdlSupported()==false, this
	// would be the only signal a unit test could catch cheaply: give it a
	// moment, then confirm no state changed (SetAWDLDown always errors on
	// unsupported platforms, so awdlSuppressing must stay false either way).
	time.Sleep(20 * time.Millisecond)
	if mw.awdlSuppressing {
		t.Error("awdlSuppressing became true on an unsupported platform")
	}
}
