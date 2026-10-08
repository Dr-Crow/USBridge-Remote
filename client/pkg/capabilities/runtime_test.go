package capabilities

import "testing"

func TestPreparedLocalProfileIsSeparateFromVendorTier(t *testing.T) {
	for _, vendor := range []string{"", "free", "pro", "enterprise"} {
		r := &RuntimeStatus{Mode: "local-research", Backend: "rustshine", StreamerPrepared: true}
		if r.EffectiveProtocol(vendor) != "local" {
			t.Fatal("vendor tier overrode prepared local mode")
		}
		r.StreamerPrepared = false
		if r.EffectiveProtocol(vendor) != vendor {
			t.Fatal("unprepared runtime gained capability")
		}
		r.StreamerPrepared = true
		r.Backend = "sunshine"
		if r.EffectiveProtocol(vendor) != vendor {
			t.Fatal("wrong backend gained local streamer identity")
		}
	}
	var r *RuntimeStatus
	if r.EffectiveProtocol("free") != "free" {
		t.Fatal("legacy compatibility changed")
	}
}
