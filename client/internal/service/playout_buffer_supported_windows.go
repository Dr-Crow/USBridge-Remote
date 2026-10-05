//go:build windows && cgo

package service

// PlayoutBufferSupported reports whether this build lets the user toggle the
// playout buffer. Also true on macOS/Linux/iOS/Android -- see their own
// playout_buffer_supported_*.go files.
func PlayoutBufferSupported() bool {
	return true
}
