package controller

import (
	"fyne.io/fyne/v2"

	"usbridge-client/internal/service"
)

// mouseCaptureEngine owns the OS-level raw relative mouse capture used by
// Capture mode: hide/pin the system cursor, read hardware deltas directly
// (bypassing Fyne's own bounded pointer events), and forward them straight to
// Moonlight via the MoonlightInputSender passed to the factory below.
//
// Unlike nativeFullscreenCapture (native_fullscreen_capture_darwin.go), this
// only ever installs *local*, app-scoped listeners -- never a global/session
// event tap -- so it needs no Accessibility/Input Monitoring permission on
// macOS, and no elevated privilege on Windows or Linux either. Keyboard
// input is never touched, so the existing Ctrl+Alt+Shift+M hotkey (video_
// widget_hotkeys.go) keeps working to release capture, exactly like
// Moonlight's own Ctrl+Alt+Shift+Z.
type mouseCaptureEngine interface {
	// Start begins capturing raw mouse input scoped to window. It must be
	// safe to call Stop even if Start returned an error.
	Start(window fyne.Window) error
	Stop() error
}

// newMouseCaptureEngine returns the platform implementation (mouse_capture_
// darwin.go / _windows.go / _linux.go / _other.go). miProvider is called on
// every event so callers can swap the underlying sender without restarting
// the engine (mirrors nativeFullscreenCapture's own miProvider pattern).
func newMouseCaptureEngine(miProvider func() service.MoonlightInputSender) mouseCaptureEngine {
	return newPlatformMouseCaptureEngine(miProvider)
}
