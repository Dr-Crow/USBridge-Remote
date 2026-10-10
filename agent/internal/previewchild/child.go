// Package previewchild is the platform launch boundary for the agent's private
// source and viewer protocols. It never interprets or logs protocol contents.
package previewchild

import (
	"errors"
	"time"

	"usbridge_agent/internal/previewprocess"
)

const StopGrace = 3 * time.Second

// Spec is supplied only after the caller's profile/hash checks. Source keeps its
// historical cooperative context cancellation on Unix; Windows always cancels
// the complete owned tree. Env is explicit on both platforms.
type Spec struct {
	Path               string
	Args, Env          []string
	CloseStdinOnCancel bool
}

var ErrStart = errors.New("preview child could not start")

// Join waits for all caller-owned protocol reads and request writes. A missed
// deadline stays a typed failure; callers must not then perform an unbounded
// Wait. Native pipe closure interrupts those operations on Windows.
func Join(done <-chan struct{}) error {
	return joinUntil(time.Now().Add(previewprocess.CleanupTimeout), done)
}

func joinUntil(deadline time.Time, channels ...<-chan struct{}) error {
	timer := time.NewTimer(max(0, time.Until(deadline)))
	defer timer.Stop()
	for _, done := range channels {
		select {
		case <-done:
			continue
		default:
		}
		select {
		case <-done:
		case <-timer.C:
			return previewprocess.ErrCleanupTimeout
		}
	}
	return nil
}
