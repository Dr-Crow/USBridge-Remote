//go:build android && cgo

package service

// PlayoutBufferSupported reports whether this build lets the user toggle the
// playout buffer. Android's own do_li_start (moonlight_cgo_android.go) sets
// CAPABILITY_DIRECT_SUBMIT the same way the shared apple/linux path does, so
// the fork's v7 in-place delay applies here too. Mobile, so it starts
// enabled by default -- see playout_buffer_default_mobile.go.
func PlayoutBufferSupported() bool {
	return true
}
