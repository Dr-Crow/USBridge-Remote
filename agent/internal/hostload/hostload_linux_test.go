//go:build linux

package hostload

import (
	"testing"
	"time"
)

func TestParseDRMFdinfo(t *testing.T) {
	i915 := "pos:\t0\nflags:\t02100002\ndrm-driver:\ti915\ndrm-pdev:\t0000:00:02.0\ndrm-client-id:\t7\ndrm-engine-render:\t25662044495 ns\ndrm-engine-copy:\t0 ns\ndrm-engine-video:\t1500000000 ns\ndrm-engine-capacity-video:\t2\n"
	id, eng := parseDRMFdinfo(i915)
	if id != "0000:00:02.0/7" || eng["render"] != 25662044495 || eng["video"] != 1500000000 || len(eng) != 3 {
		t.Fatalf("i915: %q %v", id, eng)
	}
	amd := "drm-driver:\tamdgpu\ndrm-pdev:\t0000:03:00.0\ndrm-client-id:\t12\ndrm-engine-gfx:\t1000 ns\ndrm-engine-enc:\t500 ns\ndrm-engine-dec:\t0 ns\n"
	if id, eng := parseDRMFdinfo(amd); id != "0000:03:00.0/12" || eng["enc"] != 500 {
		t.Fatalf("amdgpu: %q %v", id, eng)
	}
}

func TestDRMLoad(t *testing.T) {
	prev := map[string]drmClient{
		"a/1": {engines: map[string]uint64{"render": 0, "video": 0}, streamer: true},
		"a/2": {engines: map[string]uint64{"render": 0}},
	}
	cur := map[string]drmClient{
		"a/1": {engines: map[string]uint64{"render": 100e6, "video": 250e6}, streamer: true},
		"a/2": {engines: map[string]uint64{"render": 200e6}},
		"a/3": {engines: map[string]uint64{"render": 999e9}}, // new: no delta yet
	}
	gpu, st := drmLoad(prev, cur, 500*time.Millisecond)
	if gpu["3d"] != 60 || gpu["encode"] != 50 || st["3d"] != 20 || st["encode"] != 50 {
		t.Fatalf("gpu %v streamer %v", gpu, st)
	}
}

func TestLinuxSamplerProducesSamples(t *testing.T) {
	var s Sampler
	s.Start()
	time.Sleep(3 * Interval)
	samples := s.Stop()
	if len(samples) == 0 {
		t.Fatalf("no samples: %v", s.LastError())
	}
	t.Logf("%+v", samples[len(samples)-1])
}
