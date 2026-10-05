//go:build android || ios || (js && wasm)

package gui

import "fyne.io/fyne/v2"

// newKeepAliveWindow is a no-op on platforms that don't use Fyne's glfw
// driver. See keepalive_window_desktop.go for why desktop needs this.
func newKeepAliveWindow(a fyne.App) fyne.Window {
	return nil
}
