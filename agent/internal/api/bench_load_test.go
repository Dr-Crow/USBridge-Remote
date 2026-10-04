package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"usbridge_agent/internal/hostload"
)

// TestBenchLoadStartStopWireFormat exercises the exact JSON envelope the
// client's BenchLoadStop (client/internal/api/bench.go) decodes: {"success":
// true, "data": {"samples": [...], "error": "..."}}. benchLoadStart/Stop
// touch only s.benchLoad, so a zero-value *Server is enough -- no fake
// benchApplication needed, unlike the other /api/bench/* handlers.
func TestBenchLoadStartStopWireFormat(t *testing.T) {
	s := &Server{}

	startRec := httptest.NewRecorder()
	s.benchLoadStart(startRec, httptest.NewRequest(http.MethodPost, "/api/bench/load/start", nil))
	if startRec.Code != http.StatusOK {
		t.Fatalf("start status = %d, body = %s", startRec.Code, startRec.Body)
	}

	// At least one Interval so a platform with working sampling (Windows)
	// has a real tick to report, same reasoning as
	// hostload.TestSamplerLastErrorExplainsEmptySamples.
	time.Sleep(2 * hostload.Interval)

	stopRec := httptest.NewRecorder()
	s.benchLoadStop(stopRec, httptest.NewRequest(http.MethodPost, "/api/bench/load/stop", nil))
	if stopRec.Code != http.StatusOK {
		t.Fatalf("stop status = %d, body = %s", stopRec.Code, stopRec.Body)
	}

	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Samples []hostload.Sample `json:"samples"`
			Error   string            `json:"error,omitempty"`
		} `json:"data"`
	}
	if err := json.Unmarshal(stopRec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, stopRec.Body)
	}
	if !env.Success {
		t.Fatalf("expected a success envelope, got %s", stopRec.Body)
	}
	// samples is always present (possibly empty), per benchLoadStop's own
	// doc comment -- the client's decodeAgentResponse(&out) would silently
	// leave Samples as its zero value (nil) if the field were ever absent
	// instead of "[]", which still decodes fine, but this pins the shape.
	if env.Data.Samples == nil {
		t.Fatal("samples must be an array, never a null/absent field")
	}
	// The actual bug this test guards: an empty samples list used to carry
	// no explanation at all (see hostload.Sampler.LastError's doc comment)
	// -- the client's benchmark table just showed "n/a" for every host-load
	// row with nothing to tell "not supported on this host" apart from
	// "something broke". Every path through benchLoadStop must leave the
	// response self-explanatory.
	if len(env.Data.Samples) == 0 && env.Data.Error == "" {
		t.Fatalf("empty samples with no error explaining why: %+v", env.Data)
	}
}

// TestBenchLoadStopWithoutStart: the agent's own benchmark fallback path
// (runBenchmark's defer) calls BenchVideoStop/BenchmarkStopStream
// unconditionally on every exit, and a client that never called
// /api/bench/load/start at all must still get a well-formed response, not
// a panic, from /api/bench/load/stop.
func TestBenchLoadStopWithoutStart(t *testing.T) {
	s := &Server{}
	rec := httptest.NewRecorder()
	s.benchLoadStop(rec, httptest.NewRequest(http.MethodPost, "/api/bench/load/stop", nil))
	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %s", rec.Code, rec.Body)
	}
	var env struct {
		Success bool `json:"success"`
		Data    struct {
			Samples []hostload.Sample `json:"samples"`
		} `json:"data"`
	}
	if err := json.Unmarshal(rec.Body.Bytes(), &env); err != nil {
		t.Fatalf("decode: %v\nbody: %s", err, rec.Body)
	}
	if !env.Success || env.Data.Samples == nil {
		t.Fatalf("got %s", rec.Body)
	}
}
