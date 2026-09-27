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
func (vw *VideoWidget) BenchmarkStartStream(timeout time.Duration) (time.Duration, error) {
	// The backend may have just changed; absolute mouse mapping and the
	// session setup follow the agent's protocol.
	vw.refreshAgentProtocol()
	prevTrace := vw.videoTraceID.Load()
	start := time.Now()
	vw.handleStartVideo()

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
