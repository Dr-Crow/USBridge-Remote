//go:build !android && !ios && !(js && wasm)

package gui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/test"
)

// TestNewKeepAliveWindow_StaysTrackedAfterOtherWindowsClose proves the
// core mechanic the fix relies on: the keep-alive window occupies a slot
// in the driver's window list and that slot survives every other window
// closing. In production, this is what keeps Fyne's glfw driver window
// count above zero across the whole app lifetime, so closing the
// fullscreen dialog's secondary window (fullscreen_dialog_desktop.go's
// platformExit) can never be "the last window" and can never trigger the
// driver's unconditional Quit()-on-zero-windows path.
func TestNewKeepAliveWindow_StaysTrackedAfterOtherWindowsClose(t *testing.T) {
	app := test.NewApp()
	// test.NewApp() seeds one dummy rendering window (see test.NewDriver's
	// own doc comment) -- clear it so counts below are unambiguous.
	for _, w := range append([]fyne.Window(nil), app.Driver().AllWindows()...) {
		w.Close()
	}

	keep := newKeepAliveWindow(app)
	if keep == nil {
		t.Fatal("expected a non-nil keep-alive window on desktop")
	}
	if got := len(app.Driver().AllWindows()); got != 1 {
		t.Fatalf("expected only the keep-alive window tracked, got %d", got)
	}

	other := app.NewWindow("main")
	other.Close()

	if got := len(app.Driver().AllWindows()); got != 1 {
		t.Fatalf("expected only the keep-alive window left tracked after main closed, got %d", got)
	}
	if app.Driver().AllWindows()[0] != keep {
		t.Fatalf("expected the surviving window to be the keep-alive window itself")
	}
}
