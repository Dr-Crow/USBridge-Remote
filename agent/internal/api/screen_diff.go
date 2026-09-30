package api

import (
	"bytes"
	"fmt"
	"image/png"
)

// screenChangePercent decodes two lossless PNG screenshots and returns the
// percentage of sampled pixels whose luma differs by more than a small
// noise threshold -- the Agent's equivalent of the hardware KVM's
// gocv-based screenChangePercent (see usbridge_service/web/handlers.go),
// reimplemented with the standard library only: the Agent is a small
// cross-platform desktop binary and doesn't otherwise depend on OpenCV/gocv
// (that's specific to the device repo's video pipeline), so pulling it in
// just for this one diff would be a heavy, platform-fragile dependency to
// add for a single feature.
//
// A resolution mismatch (e.g. the host's display was reconfigured between
// captures) returns 100 -- "everything changed" is the safe/conservative
// answer, not an error, matching the hardware KVM's own behavior for the
// same case.
func screenChangePercent(beforePNG, afterPNG []byte) (float64, error) {
	before, err := png.Decode(bytes.NewReader(beforePNG))
	if err != nil {
		return 0, fmt.Errorf("decode before: %w", err)
	}
	after, err := png.Decode(bytes.NewReader(afterPNG))
	if err != nil {
		return 0, fmt.Errorf("decode after: %w", err)
	}

	bb := before.Bounds()
	ab := after.Bounds()
	if bb.Dx() != ab.Dx() || bb.Dy() != ab.Dy() {
		return 100, nil
	}
	w, h := bb.Dx(), bb.Dy()
	if w == 0 || h == 0 {
		return 0, fmt.Errorf("empty capture")
	}

	// Sample every 4th pixel in each dimension (1/16 of the frame) instead
	// of a full per-pixel scan -- at 1920x1080 that's ~130k samples instead
	// of ~2M, keeping this call fast enough to run after every click
	// without becoming the slow part of the round trip, while still being
	// far more than enough resolution to tell "something changed" from
	// "nothing changed" at the 5% threshold callers use.
	const stride = 4
	const lumaThreshold = 12 // matches the hardware KVM's own noise floor

	var changed, total int
	for y := 0; y < h; y += stride {
		for x := 0; x < w; x += stride {
			r1, g1, b1, _ := before.At(bb.Min.X+x, bb.Min.Y+y).RGBA()
			r2, g2, b2, _ := after.At(ab.Min.X+x, ab.Min.Y+y).RGBA()
			l1 := (299*int(r1>>8) + 587*int(g1>>8) + 114*int(b1>>8)) / 1000
			l2 := (299*int(r2>>8) + 587*int(g2>>8) + 114*int(b2>>8)) / 1000
			diff := l1 - l2
			if diff < 0 {
				diff = -diff
			}
			total++
			if diff > lumaThreshold {
				changed++
			}
		}
	}
	if total == 0 {
		return 0, fmt.Errorf("empty sample")
	}
	return float64(changed) / float64(total) * 100, nil
}
