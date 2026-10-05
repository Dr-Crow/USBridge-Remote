//go:build !((darwin && !ios) || (windows && cgo) || (linux && !android && cgo))

package controller

import (
	"fmt"

	"fyne.io/fyne/v2"

	"usbridge-client/internal/service"
)

// noopMouseCaptureEngine covers every platform without a real engine above
// (iOS, Android, wasm/js, and the no-cgo edge case on Windows/Linux). None
// of these ever offer Capture mode in the UI in the first place --
// mouseConfigOptions() in mouse_modes.go only lists it for
// !fyne.CurrentDevice().IsMobile(), and wasm has no equivalent raw-input API
// to hang this off of -- so Start should never actually be reached here.
type noopMouseCaptureEngine struct{}

func newPlatformMouseCaptureEngine(_ func() service.MoonlightInputSender) mouseCaptureEngine {
	return &noopMouseCaptureEngine{}
}

func (n *noopMouseCaptureEngine) Start(_ fyne.Window) error {
	return fmt.Errorf("mouse capture mode is not supported on this platform")
}

func (n *noopMouseCaptureEngine) Stop() error {
	return nil
}
