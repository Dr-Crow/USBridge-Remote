//go:build !android && !ios && !(js && wasm)

package gui

import "fyne.io/fyne/v2"

// newKeepAliveWindow creates a permanently hidden, never-closed window
// whose only job is to occupy a slot in Fyne's glfw driver window list for
// the entire app lifetime.
//
// Without it: internal/driver/glfw/loop.go's destroyWindow() calls
// d.Quit() unconditionally whenever the driver's window count reaches
// zero -- regardless of any window's SetMaster/SetCloseIntercept state.
// The fullscreen dialog (controller/fullscreen_dialog_desktop.go's
// platformExit) closes its own secondary fyne.Window every time the user
// leaves fullscreen. If, at that exact moment, the driver's window list
// had lost track of the main window for any reason, that Close() call is
// the one that drops the count to zero and silently kills the whole
// process -- no SetCloseIntercept consulted, no stream/USB disconnect, no
// crash report (it's a clean exit(0), not a signal).
//
// Reproduced in production on 2026-10-04: the app exited cleanly right as
// the user pressed Escape to leave fullscreen; the unified log's
// runningboardd entry read "termination reported by launchd (0, 0, 512)"
// -- exit status 0, no signal. See fullscreen_dialog_desktop.go's
// platformExit doc comment for the matching secondary guard.
//
// This window is created but never Show()n, so the glfw driver never
// allocates a real viewport for it (see createWindow in window.go) --
// it's a zero-cost slot, not a visible/flashing window. It's also never
// closed (the process exiting cleans it up along with everything else),
// so it can't itself be the thing that goes away.
func newKeepAliveWindow(a fyne.App) fyne.Window {
	w := a.NewWindow("")
	w.Hide()
	return w
}
