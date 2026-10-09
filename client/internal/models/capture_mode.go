package models

import "strings"

// PreferredCaptureMode picks which of modes to capture width x height with.
// A capture card can list the same size once per pixel format (MJPEG and
// YUYV); callers that only choose a size (the header resolution menu) used
// to take whichever came first -- MJPEG on most cards. YUYV wins whenever
// it is offered at this size; otherwise the first mode of this size is
// used. ok is false when no mode has this size.
func PreferredCaptureMode(modes []VideoCaptureMode, width, height int) (VideoCaptureMode, bool) {
	var first VideoCaptureMode
	found := false
	for _, m := range modes {
		if m.Width != width || m.Height != height {
			continue
		}
		if isYUYVFormat(m.PixelFormat) {
			return m, true
		}
		if !found {
			first, found = m, true
		}
	}
	return first, found
}

func isYUYVFormat(format string) bool {
	switch strings.ToUpper(strings.TrimSpace(format)) {
	case "YUYV", "YUYV422", "YUY2":
		return true
	}
	return false
}
