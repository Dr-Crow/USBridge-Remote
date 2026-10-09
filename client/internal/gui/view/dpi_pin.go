package view

import (
	"os"
	"strconv"

	"fyne.io/fyne/v2"
)

const disableDPIDetectionEnv = "FYNE_DISABLE_DPI_DETECTION"

// dpiPinBase is the monitor DPI factor folded into FYNE_SCALE once Fyne's
// own per-move DPI detection is switched off (0 = not pinned). See
// PinDetectedDPIScale.
var dpiPinBase float32

// PinDetectedDPIScale freezes Fyne's monitor DPI factor at base.
//
// Fyne's X11 driver re-detects the scale on every window move, picking the
// monitor whose rect — shrunk by that monitor's own scale — contains the
// window centre, and falling back to the primary monitor otherwise. With
// HiDPI or mirrored outputs (KDE + XWayland reports every output as
// 3840x2160+0+0) that makes the scale flip while the window moves inside a
// single monitor; the window keeps its pixel size, so the UI and the native
// video overlay are drawn at the wrong scale (video shrinks into black
// bars or overflows) until the next manual resize relayouts the canvas.
//
// Detection is disabled and base is folded into FYNE_SCALE instead, so the
// user / phone-preview scale keeps working on top of it. Skipped when the
// user already set FYNE_DISABLE_DPI_DETECTION (either way) themselves.
func PinDetectedDPIScale(base float32) bool {
	capturePreviewScaleEnv()
	if dpiPinBase > 0 || base <= 0 {
		return false
	}
	if _, set := os.LookupEnv(disableDPIDetectionEnv); set {
		return false
	}
	dpiPinBase = base
	_ = os.Setenv(disableDPIDetectionEnv, "1")
	ApplyPreviewUserScale()
	return true
}

// setUserScaleEnv writes the user-facing scale to FYNE_SCALE, multiplied by
// the pinned DPI factor when detection is off.
func setUserScaleEnv(user float32) {
	if dpiPinBase > 0 {
		user *= dpiPinBase
	}
	_ = os.Setenv(phonePreviewScaleEnv, strconv.FormatFloat(float64(user), 'f', 3, 32))
}

// originalUserScale mirrors Fyne's userScale() for the env FYNE_SCALE had
// before the phone preview / DPI pin touched it.
func originalUserScale() float32 {
	if previewScaleEnv.present && previewScaleEnv.value != "" && previewScaleEnv.value != "auto" {
		if v, err := strconv.ParseFloat(previewScaleEnv.value, 32); err == nil && v != 0 {
			return float32(v)
		}
	}
	if !previewScaleEnv.present || previewScaleEnv.value != "auto" {
		if a := fyne.CurrentApp(); a != nil && a.Settings() != nil {
			if s := a.Settings().Scale(); s > 0 {
				return s
			}
		}
	}
	return 1
}
