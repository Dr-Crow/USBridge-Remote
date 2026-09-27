package controller

import (
	"fmt"
	"time"
)

// Hooks for the streamer benchmark (gui/benchmark_runner.go): a stream
// started from nothing, timed to its first decoded frame, and stopped
// again, through the same paths the Start/Stop buttons use.

// BenchmarkStartStream starts a fresh stream and blocks until its first
// frame is decoded, returning the time from the request to that frame.
// benchmarkStartStreamFn starts the stream from the saved settings; a
// variable only so tests can observe it without a real stream.
var benchmarkStartStreamFn = (*VideoWidget).StartConfiguredVideoAsync

func (vw *VideoWidget) BenchmarkStartStream(timeout time.Duration) (time.Duration, error) {
	// The backend may have just changed; absolute mouse mapping and the
	// session setup follow the agent's protocol.
	vw.refreshAgentProtocol()
	prevTrace := vw.videoTraceID.Load()
	start := time.Now()
	// Not handleStartVideo: that's the Start button's handler and opens the
	// video settings dialog, which popped up mid-benchmark, covered (and on
	// Windows hid) the stream being measured, never started it if closed,
	// and counted the operator's click as startup time. Start straight
	// from the saved settings instead, like autostart does.
	vw.userStoppedVideo.Store(false)
	benchmarkStartStreamFn(vw)

	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	for {
		select {
		case <-deadline.C:
			return 0, fmt.Errorf("no video within %v", timeout)
		case <-ticker.C:
			if vw.isClosing.Load() {
				return 0, fmt.Errorf("video closed")
			}
			if vw.videoTraceID.Load() > prevTrace {
				// beginVideoTrace bumps the ID before zeroing the
				// first-frame stamp; ignore a stale one from before start.
				if ns := vw.videoTraceFirstFrame.Load(); ns != 0 && ns >= start.UnixNano() {
					return time.Unix(0, ns).Sub(start), nil
				}
			}
		}
	}
}

// BenchmarkStopStream stops the stream and waits for the session to close.
func (vw *VideoWidget) BenchmarkStopStream() {
	_ = vw.StopVideoSync()
}

// BenchmarkStreamConfig is the configured stream fps and resolution.
func (vw *VideoWidget) BenchmarkStreamConfig() (fps, width, height int) {
	cfg := loadSavedVideoDeviceConfig(selectedVideoDevicePath(), "")
	return cfg.VideoFPS, cfg.VideoWidth, cfg.VideoHeight
}
