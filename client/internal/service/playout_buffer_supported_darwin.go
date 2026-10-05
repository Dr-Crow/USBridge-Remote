//go:build darwin && !ios && cgo

package service

// PlayoutBufferSupported reports whether this build lets the user toggle the
// playout buffer. macOS (alongside Windows, see
// playout_buffer_supported_windows.go): the fork's adaptive delay runs on
// the DIRECT_SUBMIT path here too (VideoDepacketizer.c's reassembleFrame,
// "v7" doc comment), so flipping USBRIDGE_PLAYOUT_BUFFER actually does
// something -- it's wired from moonlight_cgo_wrapper.go, shared with
// iOS/Linux, which only touches the env var when this returns true.
func PlayoutBufferSupported() bool {
	return true
}
