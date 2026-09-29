package service

import (
	"bytes"
	"testing"
)

// The benchmark reports its steps in the HUD instead of a popup (a popup
// over the stream hides the native video on Windows), so the banner must
// actually be drawn, and leave no trace once cleared.
func TestNetGraphBannerDrawnAndCleared(t *testing.T) {
	t.Cleanup(func() { SetNetGraphBanner("") })
	samples := make([]NetGraphSample, 10)
	for i := range samples {
		samples[i] = NetGraphSample{RTTValid: true, RTTMs: 10}
	}

	SetNetGraphBanner("")
	base := buildNetGraphHUD(samples).Pix

	const text = "Sunshine: measuring, 42 s left  37%"
	SetNetGraphBanner(text)
	if got := NetGraphBanner(); got != text {
		t.Fatalf("NetGraphBanner() = %q, want %q", got, text)
	}
	with := buildNetGraphHUD(samples).Pix
	diff := 0
	for i := range base {
		if base[i] != with[i] {
			diff++
		}
	}
	if diff < 200 {
		t.Fatalf("banner not drawn: only %d bytes of the HUD changed", diff)
	}

	SetNetGraphBanner("")
	if !bytes.Equal(buildNetGraphHUD(samples).Pix, base) {
		t.Fatal("HUD differs from the no-banner HUD after clearing the banner")
	}
}
