package controller

import (
	"strings"

	"fyne.io/fyne/v2"

	"usbridge-client/internal/gui/i18n"
)

const (
	mouseModeTouchPad      = "mouse"
	mouseModeTouchScreen   = "touchscreen"
	mouseModeAbsolute      = "absolute"
	mouseModeVirtualCursor = "cursor"  // Android-only: local cursor rendered in Vulkan
	mouseModeGyroMouse     = "gyro"    // Android-only: cursor via gyroscope + swipes; volume=LMB/RMB
	mouseModeCapture       = "capture" // Desktop-only: OS-level raw relative capture (Moonlight-style)

	MouseModeTouchPad      = mouseModeTouchPad
	MouseModeTouchScreen   = mouseModeTouchScreen
	MouseModeAbsolute      = mouseModeAbsolute
	MouseModeVirtualCursor = mouseModeVirtualCursor
	MouseModeGyroMouse     = mouseModeGyroMouse
	MouseModeCapture       = mouseModeCapture
)

func defaultMouseMode() string {
	if fyne.CurrentDevice().IsMobile() {
		return mouseModeVirtualCursor
	}
	return mouseModeAbsolute
}

func parseMouseMode(mode string) (string, bool) {
	mode = strings.TrimSpace(strings.ToLower(mode))
	switch {
	case strings.HasPrefix(mode, "mouse:"):
		return mouseModeTouchPad, true
	case strings.HasPrefix(mode, "touchscreen:"):
		return mouseModeTouchScreen, true
	case strings.HasPrefix(mode, "absolute:"):
		return mouseModeAbsolute, true
	case strings.HasPrefix(mode, "cursor:"):
		return mouseModeVirtualCursor, true
	case strings.HasPrefix(mode, "gyro:"):
		return mouseModeGyroMouse, true
	case strings.HasPrefix(mode, "capture:"):
		return mouseModeCapture, true
	}

	switch mode {
	case mouseModeTouchPad, mouseModeTouchScreen, mouseModeAbsolute, mouseModeVirtualCursor, mouseModeGyroMouse, mouseModeCapture:
		return mode, true
	case "double":
		return mouseModeAbsolute, true
	default:
		return "", false
	}
}

func normalizeMouseMode(mode string) string {
	if normalized, ok := parseMouseMode(mode); ok {
		if normalized == mouseModeTouchScreen {
			return mouseModeTouchPad
		}
		return normalized
	}
	return defaultMouseMode()
}

func isVirtualCursorMode(mode string) bool {
	return normalizeMouseMode(mode) == mouseModeVirtualCursor
}

// isVirtualCursorLikeMode returns true for both cursor and gyro modes —
// both render a local Vulkan cursor and send absolute positions to Moonlight.
func isVirtualCursorLikeMode(mode string) bool {
	m := normalizeMouseMode(mode)
	return m == mouseModeVirtualCursor || m == mouseModeGyroMouse
}

func mouseTransportType(mode string) string {
	normalized := normalizeMouseMode(mode)
	// Virtual cursor and gyro mouse are client-only concepts: the USB gadget and
	// server bridge must be configured in absolute mode so LiSendMousePositionEvent
	// is routed through bridgeAbsMouse → HID tablet on the host.
	if normalized == mouseModeVirtualCursor || normalized == mouseModeGyroMouse {
		return mouseModeAbsolute
	}
	// Capture is also client-only: it's touchpad's own relative wire path
	// (LiSendMouseMoveEvent) fed by a native OS capture engine instead of
	// Fyne drag events, so the gadget/agent side needs no new transport.
	if normalized == mouseModeCapture {
		return mouseModeTouchPad
	}
	return normalized
}

func isMouseDeviceType(deviceType string) bool {
	_, ok := parseMouseMode(deviceType)
	return ok
}

func mouseModeFromDeviceType(deviceType string) string {
	if mode, ok := parseMouseMode(deviceType); ok {
		return mode
	}
	return defaultMouseMode()
}

// mouseConfigOptions lists the pointer modes offered in the mouse mode
// pickers (device row, footer icon menu). Touchpad is mobile/touch-only --
// on desktop there's no finger to lift-and-reposition, so only Absolute and
// Capture are offered there.
func mouseConfigOptions() []string {
	if fyne.CurrentDevice().IsMobile() {
		return []string{
			i18n.Current.DeviceTouchPad,
			i18n.Current.DeviceAbsolute,
			i18n.Current.DeviceVirtualCursor,
			i18n.Current.DeviceGyroMouse,
		}
	}
	return []string{
		i18n.Current.DeviceAbsolute,
		i18n.Current.DeviceCapture,
	}
}

func mouseConfigToLabel(mode string, dispIdx, dispCnt int) string {
	switch normalizeMouseMode(mode) {
	case mouseModeAbsolute:
		return i18n.Current.DeviceAbsolute
	case mouseModeVirtualCursor:
		return i18n.Current.DeviceVirtualCursor
	case mouseModeGyroMouse:
		return i18n.Current.DeviceGyroMouse
	case mouseModeCapture:
		return i18n.Current.DeviceCapture
	default:
		return i18n.Current.DeviceTouchPad
	}
}

func mouseLabelToConfig(s string) (mode string, dispIdx, dispCnt int) {
	switch s {
	case i18n.Current.DeviceAbsolute:
		return mouseModeAbsolute, 0, 1
	case i18n.Current.DeviceVirtualCursor:
		return mouseModeVirtualCursor, 0, 1
	case i18n.Current.DeviceGyroMouse:
		return mouseModeGyroMouse, 0, 1
	case i18n.Current.DeviceCapture:
		return mouseModeCapture, 0, 1
	default:
		return mouseModeTouchPad, 0, 1
	}
}
