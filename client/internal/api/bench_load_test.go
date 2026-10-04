package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// localHostLoadSample mirrors service.BenchLoad's JSON shape (client/
// internal/service/bench_recorder.go) without importing that package --
// service already imports api (for USBClient), so importing it back here
// would cycle. Field-for-field match with agent/internal/hostload.Sample is
// what actually matters; this struct exists purely so the test can decode
// BenchLoadStop's raw JSON the same way benchmark_runner.go's
// json.Unmarshal(raw, &run.HostLoad) does.
type localHostLoadSample struct {
	AtMs        int64              `json:"at_ms"`
	CPU         float64            `json:"cpu"`
	StreamerCPU float64            `json:"streamer_cpu"`
	GPU         map[string]float64 `json:"gpu,omitempty"`
	StreamerGPU map[string]float64 `json:"streamer_gpu,omitempty"`
}

// newTestUSBClient (points a *USBClient at an httptest.Server) is already
// defined in usb_client_datachannel_test.go, same package.

// TestBenchLoadStopDecodesAgentSamples is the client-side half of the
// server/client wire contract agent/internal/api/bench_load_test.go's
// TestBenchLoadStartStopWireFormat covers from the agent's side: given
// exactly the envelope benchLoadStop produces for a successful run, the
// client must hand back a samples payload that decodes into
// service.BenchLoad (mirrored here as localHostLoadSample) with every
// field intact, field-for-field, the same way benchmark_runner.go's
// stopLoad closure does.
func TestBenchLoadStopDecodesAgentSamples(t *testing.T) {
	const agentBody = `{"success":true,"message":"bench_load","data":{"samples":[` +
		`{"at_ms":0,"cpu":12.5,"streamer_cpu":3.25,"gpu":{"3d":40.1,"encode":55.6},"streamer_gpu":{"3d":10.2,"encode":55.6}},` +
		`{"at_ms":500,"cpu":14.0,"streamer_cpu":4.0,"gpu":{"3d":41.0,"encode":56.0},"streamer_gpu":{"3d":11.0,"encode":56.0}}` +
		`]}}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/bench/load/stop" {
			t.Errorf("unexpected path %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(agentBody))
	}))
	defer upstream.Close()

	client := newTestUSBClient(t, upstream)
	raw, err := client.BenchLoadStop()
	if err != nil {
		t.Fatalf("BenchLoadStop: %v", err)
	}

	var samples []localHostLoadSample
	if err := json.Unmarshal(raw, &samples); err != nil {
		t.Fatalf("decode samples: %v\nraw: %s", err, raw)
	}
	if len(samples) != 2 {
		t.Fatalf("got %d samples, want 2: %+v", len(samples), samples)
	}
	if samples[0].CPU != 12.5 || samples[0].StreamerCPU != 3.25 {
		t.Fatalf("sample[0] cpu/streamer_cpu: %+v", samples[0])
	}
	if samples[0].GPU["encode"] != 55.6 || samples[1].GPU["3d"] != 41.0 {
		t.Fatalf("GPU maps not decoded correctly: %+v", samples)
	}
	if samples[1].AtMs != 500 {
		t.Fatalf("sample[1].AtMs = %d, want 500", samples[1].AtMs)
	}
}

// TestBenchLoadStopSurfacesAgentError is the regression test for the bug
// this session fixed: an agent host where sampling genuinely isn't
// available (hostload.Sampler.LastError, e.g. "not implemented on this
// platform", or a real PDH failure on Windows) used to come back as a
// bare empty samples list -- indistinguishable, from the client's side,
// from "it's just early, no ticks yet". BenchLoadStop must now return a
// non-nil error whenever the agent named one, so stopLoad's
// logrus.Warnf actually explains the resulting "n/a" instead of the
// reason only ever reaching the agent's own log file.
func TestBenchLoadStopSurfacesAgentError(t *testing.T) {
	const agentBody = `{"success":true,"message":"bench_load","data":{"samples":[],"error":"not implemented on this platform"}}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(agentBody))
	}))
	defer upstream.Close()

	client := newTestUSBClient(t, upstream)
	_, err := client.BenchLoadStop()
	if err == nil {
		t.Fatal("expected a non-nil error when the agent reports samples=[] with an explanation, got nil")
	}
}

// TestBenchLoadStopEmptyWithoutErrorIsNotAnError: an agent predating the
// "error" field, or one that simply hasn't ticked yet, must still decode
// cleanly into zero samples with err == nil -- benchLoadAverages already
// treats zero samples as "not valid" on its own; BenchLoadStop must not
// pile a synthetic error on top of a case that was never actually an
// agent-side failure.
func TestBenchLoadStopEmptyWithoutErrorIsNotAnError(t *testing.T) {
	const agentBody = `{"success":true,"message":"bench_load","data":{"samples":[]}}`

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.Write([]byte(agentBody))
	}))
	defer upstream.Close()

	client := newTestUSBClient(t, upstream)
	raw, err := client.BenchLoadStop()
	if err != nil {
		t.Fatalf("expected no error, got %v", err)
	}
	var samples []localHostLoadSample
	if err := json.Unmarshal(raw, &samples); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if len(samples) != 0 {
		t.Fatalf("expected 0 samples, got %d", len(samples))
	}
}
