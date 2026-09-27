package app

import (
	"usbridge_agent/internal/benchvideo"
)

// BenchPlayer is the streamer benchmark's host-side video player (see
// internal/benchvideo), created on first use.
func (a *App) BenchPlayer() *benchvideo.Player {
	a.benchOnce.Do(func() {
		a.benchPlayer = benchvideo.New(a.cfg.StateDir)
	})
	return a.benchPlayer
}

// BenchStreamBackends reports the active stream backend and which ones a
// benchmark can switch to right now.
func (a *App) BenchStreamBackends() (active string, available []string) {
	available = []string{"sunshine"}
	if a.rustshineStaged() {
		available = append(available, "rustshine")
	}
	return a.currentStreamKind(), available
}
