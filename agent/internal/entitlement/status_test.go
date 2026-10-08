package entitlement

import "testing"

func TestStatusProtocol(t *testing.T) {
	t.Parallel()
	cases := []struct {
		backend, tier, want string
	}{
		{"sunshine", "", "opensource"},
		{"sunshine", "pro", "opensource"},
		{"", "enterprise", "opensource"},
		{"rustshine", "", "free"},
		{"rustshine", "free", "free"},
		{"rustshine", "Pro", "pro"},
		{"rustshine", "enterprise", "enterprise"},
	}
	for _, tc := range cases {
		got := Status{ActiveBackend: tc.backend, Tier: tc.tier}.Protocol()
		if got != tc.want {
			t.Fatalf("backend=%q tier=%q: got %q, want %q", tc.backend, tc.tier, got, tc.want)
		}
	}
}

func TestLocalMetadataDoesNotChangeVendorTier(t *testing.T) {
	for _, tier := range []string{"", "free", "expired"} {
		s := Status{ActiveBackend: "rustshine", Tier: tier, LocalRuntimeActive: true, LocalRuntimeStreamerPrepared: true}
		if s.Protocol() != "free" {
			t.Fatal("vendor tier changed")
		}
		if s.RuntimeMetadata().EffectiveProtocol(s.Protocol()) != "local" {
			t.Fatal("prepared runtime hidden")
		}
		s.LocalRuntimeStreamerPrepared = false
		if s.RuntimeMetadata().EffectiveProtocol(s.Protocol()) != "free" {
			t.Fatal("unprepared runtime promoted")
		}
		s.LocalRuntimeActive = false
		if s.RuntimeMetadata() != nil {
			t.Fatal("inactive runtime exposed")
		}
	}
}

func TestPreparedRuntimeProjectsLegacyClientProtocolWithoutChangingTier(t *testing.T) {
	for _, tier := range []string{"", "free", "expired", "pro", "enterprise"} {
		s := Status{ActiveBackend: "rustshine", Tier: tier, LocalRuntimeActive: true, LocalRuntimeStreamerPrepared: true}
		if s.ClientProtocol() != "pro" || s.Tier != tier {
			t.Fatal("incorrect compatibility projection")
		}
		if s.RuntimeMetadata().EffectiveProtocol(s.ClientProtocol()) != "local" {
			t.Fatal("source client lost local identity")
		}
		s.LocalRuntimeStreamerPrepared = false
		if s.ClientProtocol() != s.Protocol() {
			t.Fatal("unprepared runtime promoted")
		}
	}
	s := Status{ActiveBackend: "sunshine", Tier: "free", LocalRuntimeActive: true, LocalRuntimeStreamerPrepared: true, LocalRuntimeUSBPrepared: true}
	if s.ClientProtocol() != "opensource" {
		t.Fatal("Sunshine was misidentified as RustShine")
	}
	s.ActiveBackend = "rustshine"
	s.LocalRuntimeActive = false
	if s.ClientProtocol() != "free" {
		t.Fatal("inactive runtime promoted")
	}
}
