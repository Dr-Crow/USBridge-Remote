package service

import "sync/atomic"

// The playout (jitter) buffer in our moonlight-common-c fork
// (VideoDepacketizer.c's playoutDelayForFrame) holds each frame back by a
// multiple of the measured network jitter before decoding it. It smooths
// cadence on a jittery link, but it is pure added latency, and the official
// Moonlight clients have nothing like it -- so on Windows it is opt-in from
// the video settings dialog and off by default. See
// docs/WINDOWS_DECODE_PIPELINE.md for the measurements behind this.
var playoutBufferEnabled atomic.Bool

// SetPlayoutBufferEnabled turns the buffer on or off for the next stream
// start (the fork reads it once per session). The video settings dialog
// applies this from Apply/Start; Cancel/close leaves the previous state.
func SetPlayoutBufferEnabled(enabled bool) {
	playoutBufferEnabled.Store(enabled)
}

// PlayoutBufferEnabled reports the checkbox's current state.
func PlayoutBufferEnabled() bool {
	return playoutBufferEnabled.Load()
}
