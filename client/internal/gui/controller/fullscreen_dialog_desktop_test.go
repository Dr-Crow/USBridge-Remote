//go:build !android && !ios && !(js && wasm)

package controller

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// closeAllWindows clears test.NewApp()'s baked-in dummy rendering window
// (see test.NewDriver's own doc comment) so each test starts from a known,
// zero window count instead of an incidental one.
func closeAllWindows(app fyne.App) {
	for _, w := range append([]fyne.Window(nil), app.Driver().AllWindows()...) {
		w.Close()
	}
}

// TestPlatformExit_RefusesToCloseLastTrackedWindow reproduces the exact
// window-bookkeeping state that silently killed the production client on
// 2026-10-04 when Escape was pressed to leave fullscreen: the fullscreen
// window was the only window Fyne's driver still had in its list.
//
// The real glfw driver calls Quit() unconditionally when that count hits
// zero (internal/driver/glfw/loop.go's destroyWindow) -- that specific
// call isn't reachable from a headless test (it needs a real display),
// so this instead proves the decision logic in platformExit() never
// reaches Close() while in that state, which is the part we can actually
// control and must get right regardless of how the count got there.
func TestPlatformExit_RefusesToCloseLastTrackedWindow(t *testing.T) {
	app := test.NewApp()
	closeAllWindows(app) // test.NewApp() seeds one dummy rendering window

	win := app.NewWindow("fullscreen")
	fd := &FullscreenDialog{fullscreenWindow: win}

	fd.platformExit()

	all := fyne.CurrentApp().Driver().AllWindows()
	if len(all) != 1 {
		t.Fatalf("expected the window to still be tracked (Hidden, not Closed); AllWindows() = %d", len(all))
	}
	if all[0] != win {
		t.Fatalf("expected the tracked window to still be the fullscreen window itself")
	}
	if fd.fullscreenWindow != nil {
		t.Fatalf("expected fd.fullscreenWindow to be cleared")
	}
}

// TestPlatformExit_ClosesNormallyWithOtherWindowsOpen is the companion
// case: when another window (e.g. the main window) is still tracked,
// closing the fullscreen window is safe and should actually happen --
// the guard must not become a permanent leak of every fullscreen window.
func TestPlatformExit_ClosesNormallyWithOtherWindowsOpen(t *testing.T) {
	app := test.NewApp()
	closeAllWindows(app) // test.NewApp() seeds one dummy rendering window

	main := app.NewWindow("main")
	fs := app.NewWindow("fullscreen")
	fd := &FullscreenDialog{fullscreenWindow: fs}

	fd.platformExit()

	all := fyne.CurrentApp().Driver().AllWindows()
	if len(all) != 1 {
		t.Fatalf("expected exactly 1 window left (main), got %d", len(all))
	}
	if all[0] != main {
		t.Fatalf("expected the remaining window to be the main window")
	}
}
