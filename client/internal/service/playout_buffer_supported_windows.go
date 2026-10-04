//go:build windows && cgo

package service

// PlayoutBufferSupported reports whether this build lets the user toggle the
// playout buffer. Windows only: the other platforms keep their current
// always-on behavior (macOS's Metal path still decodes on the receive thread
// and relies on it -- see moonlight_cgo_shared.h).
func PlayoutBufferSupported() bool {
	return true
}
