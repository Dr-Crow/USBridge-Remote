//go:build windows

package previeweligibility

import (
	"encoding/json"
	"testing"
)

func TestWindowsReadOnlyEligibilityObservation(t *testing.T) {
	s := Observe()
	raw, err := json.Marshal(struct {
		Snapshot
		Eligible bool `json:"eligible"`
	}{s, s.Eligible()})
	if err != nil {
		t.Fatal("closed eligibility encoding failed")
	}
	t.Log("PREVIEW_ELIGIBILITY " + string(raw))
	if !s.TokenQueried || !s.TokenClosed || !s.WindowStationQueried {
		t.Fatal("read-only eligibility query failed")
	}
	// Elevated or noninteractive CI is a reported ineligible environment. This
	// observation neither bypasses the policy nor changes manager enablement.
}
