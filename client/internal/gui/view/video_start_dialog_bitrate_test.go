package view

import "testing"

// TestBitrateRangeFollowsPyroWave: picking PyroWave moves the slider to its own
// range and back, and each range keeps the value last picked in it.
func TestBitrateRangeFollowsPyroWave(t *testing.T) {
	vsd := &VideoStartDialog{
		bitrateSlider:  newVideoDialogBitrateSlider(bitrateMinKbps, bitrateMaxKbps, bitrateStepKbps),
		bitrateByRange: map[bool]float64{false: bitrateDefaultKbps, true: pyroWaveBitrateDefault},
	}
	s := vsd.bitrateSlider
	s.SetValue(80000)

	vsd.applyBitrateRange(true)
	if s.Min != pyroWaveBitrateMinKbps || s.Max != pyroWaveBitrateMaxKbps || s.Value != pyroWaveBitrateDefault {
		t.Fatalf("PyroWave range: min %v max %v value %v", s.Min, s.Max, s.Value)
	}
	s.SetValue(500000)

	vsd.applyBitrateRange(false)
	if s.Max != bitrateMaxKbps || s.Value != 80000 {
		t.Fatalf("regular range: max %v value %v, want the 80000 picked before", s.Max, s.Value)
	}
	vsd.applyBitrateRange(true)
	if s.Value != 500000 {
		t.Fatalf("back on PyroWave: value %v, want 500000", s.Value)
	}
}
