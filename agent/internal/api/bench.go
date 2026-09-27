package api

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	"time"

	"usbridge_agent/internal/benchvideo"
)

// benchApplication is what the streamer benchmark needs from the app
// beyond Application. Kept separate (and type-asserted) so test fakes of
// Application don't have to grow benchmark methods.
type benchApplication interface {
	BenchPlayer() *benchvideo.Player
	BenchStreamBackends() (active string, available []string)
	SetStreamBackend(kind string) error
}

// BenchStatus is GET /api/bench/status.
type BenchStatus struct {
	ActiveBackend     string          `json:"active_backend"`
	AvailableBackends []string        `json:"available_backends"`
	Video             benchvideo.Info `json:"video"`
}

func (s *Server) benchApp(w http.ResponseWriter) (benchApplication, bool) {
	b, ok := s.app.(benchApplication)
	if !ok {
		s.fail(w, http.StatusNotImplemented, "bench_unsupported", errors.New("this agent build has no benchmark support"))
	}
	return b, ok
}

func (s *Server) benchStatus(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	active, available := b.BenchStreamBackends()
	s.ok(w, "bench_status", BenchStatus{
		ActiveBackend:     active,
		AvailableBackends: available,
		Video:             b.BenchPlayer().Status(),
	})
}

// benchBackend switches the stream backend and reports how long the agent
// took to stop the old one and bring the new one up to a bound listener
// (SetStreamBackend already waits for that). The client times its own
// stream start separately, from the moment this returns.
func (s *Server) benchBackend(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	var req struct {
		Kind string `json:"kind"`
	}
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		s.fail(w, http.StatusBadRequest, "invalid_json", err)
		return
	}
	// The video would otherwise keep playing across the switch and give
	// the next backend a head start on the content.
	b.BenchPlayer().Stop()
	start := time.Now()
	if err := b.SetStreamBackend(req.Kind); err != nil {
		log.Printf("[api] bench backend switch to %q failed: %v", req.Kind, err)
		s.fail(w, http.StatusInternalServerError, "bench_backend_failed", err)
		return
	}
	switchMs := time.Since(start).Milliseconds()
	active, _ := b.BenchStreamBackends()
	log.Printf("[api] bench backend switched to %s in %dms", active, switchMs)
	s.ok(w, "bench_backend", map[string]any{"active_backend": active, "switch_ms": switchMs})
}

// benchPrepare downloads the benchmark content ahead of the timed runs.
func (s *Server) benchPrepare(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	path, err := b.BenchPlayer().Prepare(ctx)
	if err != nil {
		// Not fatal: Start falls back to a generated pattern.
		s.ok(w, "bench_prepare", map[string]any{"ready": false, "error": err.Error()})
		return
	}
	s.ok(w, "bench_prepare", map[string]any{"ready": true, "content_path": path})
}

func (s *Server) benchVideoStart(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 3*time.Minute)
	defer cancel()
	info, err := b.BenchPlayer().Start(ctx)
	if err != nil {
		log.Printf("[api] bench video start failed: %v", err)
		s.fail(w, http.StatusInternalServerError, "bench_video_failed", err)
		return
	}
	s.ok(w, "bench_video_started", info)
}

func (s *Server) benchVideoStop(w http.ResponseWriter, r *http.Request) {
	b, ok := s.benchApp(w)
	if !ok {
		return
	}
	b.BenchPlayer().Stop()
	s.ok(w, "bench_video_stopped", nil)
}
