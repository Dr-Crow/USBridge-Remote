package previeweligibility

import "testing"

func TestEligibilityRequiresEveryIndependentFact(t *testing.T) {
	for mask := 0; mask < 64; mask++ {
		s := Snapshot{TokenQueried: mask&1 != 0, NotElevated: mask&2 != 0, InteractiveSession: mask&4 != 0, TokenClosed: mask&8 != 0, WindowStationQueried: mask&16 != 0, WindowStationVisible: mask&32 != 0}
		if s.Eligible() != (mask == 63) {
			t.Fatalf("eligibility mask %d", mask)
		}
	}
}
