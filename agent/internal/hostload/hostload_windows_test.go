//go:build windows

package hostload

import (
	"os"
	"testing"
	"time"
)

func TestParseGPUInstance(t *testing.T) {
	pid, engine, typ, ok := parseGPUInstance("pid_1234_luid_0x00000000_0x0000D1A5_phys_0_eng_3_engtype_VideoEncode")
	if !ok || pid != 1234 || engine != "luid_0x00000000_0x0000D1A5_phys_0_eng_3" || typ != "encode" {
		t.Fatalf("got %d %q %q %v", pid, engine, typ, ok)
	}
	if _, _, typ, _ := parseGPUInstance("pid_1_luid_0x0_0x1_phys_0_eng_0_engtype_3D"); typ != "3d" {
		t.Fatalf("3D -> %q", typ)
	}
	if _, _, _, ok := parseGPUInstance("_Total"); ok {
		t.Fatal("parsed a non-engine instance")
	}
}

func TestGPUByEngineTypeSumsProcessesPerEngine(t *testing.T) {
	names := []string{
		"pid_1_luid_a_phys_0_eng_0_engtype_3D",
		"pid_2_luid_a_phys_0_eng_0_engtype_3D",
		"pid_2_luid_a_phys_0_eng_1_engtype_VideoEncode",
		"pid_2_luid_b_phys_0_eng_0_engtype_3D",
	}
	items := []pdhFmtCounterValueItem{{value: 10}, {value: 15}, {value: 30}, {value: 5}}
	all, own := gpuByEngineType(names, items, map[int]bool{2: true})
	if all["3d"] != 25 || all["encode"] != 30 || own["3d"] != 15 || own["encode"] != 30 {
		t.Fatalf("all=%v own=%v", all, own)
	}
}

// HOSTLOAD_LIVE=1 samples the real machine for two seconds.
func TestLiveSampling(t *testing.T) {
	if os.Getenv("HOSTLOAD_LIVE") == "" {
		t.Skip("set HOSTLOAD_LIVE=1")
	}
	var s Sampler
	s.Start()
	time.Sleep(2 * time.Second)
	samples := s.Stop()
	if len(samples) == 0 {
		t.Fatal("no samples")
	}
	for _, x := range samples {
		t.Logf("%+v", x)
	}
}
