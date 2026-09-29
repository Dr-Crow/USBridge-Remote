// Package hostload samples the host's CPU and GPU load for the streamer
// benchmark: whole-machine CPU, the streamer processes' CPU, and GPU
// utilization per engine type (3D, video encode, video decode, copy), for
// the whole machine and for the streamer processes alone. Windows reads the
// same performance counters Task Manager shows; elsewhere Start works and
// the run simply has no samples.
package hostload

import (
	"strings"
	"sync"
	"time"
)

// Sample is one reading. CPU is a share of the whole machine (all cores =
// 100); GPU is per engine type as Task Manager shows it (the busiest
// engine of that type), keyed "3d", "encode", "decode", "copy", ...
type Sample struct {
	AtMs        int64              `json:"at_ms"` // since Start
	CPU         float64            `json:"cpu"`
	StreamerCPU float64            `json:"streamer_cpu"`
	GPU         map[string]float64 `json:"gpu,omitempty"`
	StreamerGPU map[string]float64 `json:"streamer_gpu,omitempty"`
}

// Interval is how often a running Sampler reads the counters.
const Interval = 500 * time.Millisecond

// streamerNames are the streamer processes' image names, lower case,
// without ".exe".
var streamerNames = []string{"sunshine", "usbridge-streamer"}

func isStreamer(name string) bool {
	name = strings.TrimSuffix(strings.ToLower(name), ".exe")
	// Performance counters name a second instance "sunshine#1".
	if i := strings.IndexByte(name, '#'); i >= 0 {
		name = name[:i]
	}
	for _, n := range streamerNames {
		if name == n {
			return true
		}
	}
	return false
}

// Sampler records samples between Start and Stop.
type Sampler struct {
	mu      sync.Mutex
	samples []Sample
	stop    chan struct{}
	done    chan struct{}
}

// Start begins a new recording, dropping any previous one.
func (s *Sampler) Start() {
	s.Stop()
	s.mu.Lock()
	defer s.mu.Unlock()
	s.samples = nil
	s.stop = make(chan struct{})
	s.done = make(chan struct{})
	go s.loop(s.stop, s.done)
}

// Stop ends the recording and returns its samples.
func (s *Sampler) Stop() []Sample {
	s.mu.Lock()
	stop, done := s.stop, s.done
	s.stop, s.done = nil, nil
	s.mu.Unlock()
	if stop == nil {
		return nil
	}
	close(stop)
	<-done
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.samples
	s.samples = nil
	return out
}

func (s *Sampler) loop(stop, done chan struct{}) {
	defer close(done)
	r, err := newReader()
	if err != nil {
		logf("host load sampling unavailable: %v", err)
		return
	}
	defer r.close()
	start := time.Now()
	ticker := time.NewTicker(Interval)
	defer ticker.Stop()
	for {
		select {
		case <-stop:
			return
		case <-ticker.C:
		}
		sample, err := r.read()
		if err != nil {
			continue
		}
		sample.AtMs = time.Since(start).Milliseconds()
		s.mu.Lock()
		s.samples = append(s.samples, sample)
		s.mu.Unlock()
	}
}
