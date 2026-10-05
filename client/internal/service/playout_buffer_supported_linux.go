//go:build linux && !android && cgo

package service

// PlayoutBufferSupported reports whether this build lets the user toggle the
// playout buffer. Linux shares moonlight_cgo_shared.h's DIRECT_SUBMIT path
// with macOS (see playout_buffer_supported_darwin.go), so the same v7
// in-place delay applies here too. Desktop, so off by default like Windows
// and macOS (see playout_buffer.go's zero-value default).
func PlayoutBufferSupported() bool {
	return true
}
