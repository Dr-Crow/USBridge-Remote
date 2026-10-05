//go:build android || ios

package service

// On desktop (Windows/macOS/Linux) the jitter buffer starts off --
// playoutBufferEnabled's zero value -- matching the official Moonlight
// clients and leaving it opt-in for a bad connection (see playout_buffer.go).
// Mobile connections are commonly cellular or variable Wi-Fi, so it starts
// on here instead; the checkbox still lets the user turn it off on a good
// connection.
func init() {
	SetPlayoutBufferEnabled(true)
}
