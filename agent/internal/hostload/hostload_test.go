package hostload

import (
	"testing"
	"time"
)

// TestSamplerLastErrorExplainsEmptySamples runs cross-platform (unlike
// hostload_windows_test.go's PDH-specific tests): on Windows a working
// newReader produces real samples within one Interval and LastError is
// nil; everywhere else newReader (hostload_other.go) fails immediately and
// LastError must name why -- this is exactly the gap
// internal/api/bench.go's benchLoadStop now closes, so an empty samples
// list is never silently unexplained.
func TestSamplerLastErrorExplainsEmptySamples(t *testing.T) {
	var s Sampler
	s.Start()
	time.Sleep(2 * Interval) // at least one tick on a working reader
	samples := s.Stop()

	if len(samples) == 0 && s.LastError() == nil {
		t.Fatal("zero samples but LastError is nil -- a caller has no way to tell \"not supported here\" from \"silently broken\"")
	}
	if len(samples) > 0 && s.LastError() != nil {
		t.Fatalf("got %d samples but LastError = %v -- a successful tick must clear it", len(samples), s.LastError())
	}
}

// TestSamplerStartClearsPreviousLastError: a failed run's LastError must
// not leak into a later run that actually worked (or that simply hasn't
// failed yet) -- Start resets it before the new loop goroutine starts.
func TestSamplerStartClearsPreviousLastError(t *testing.T) {
	var s Sampler
	s.Start()
	s.Stop() // too short to collect anything; likely leaves LastError set

	s.Start()
	if err := s.LastError(); err != nil {
		t.Fatalf("Start must clear the previous run's LastError immediately, got %v", err)
	}
	s.Stop()
}

// TestSamplerStopWithoutStartIsSafe: the API handlers call Stop
// unconditionally in some paths; it must never panic on a Sampler that
// was never Start()ed.
func TestSamplerStopWithoutStartIsSafe(t *testing.T) {
	var s Sampler
	if samples := s.Stop(); samples != nil {
		t.Fatalf("Stop on a never-started Sampler returned %v, want nil", samples)
	}
}
