//go:build darwin && !ios && cgo && metaltest

package service

import "testing"

// TestMetalVideoCreateReplaceInvalidatesPriorDisplayLink is a regression
// test for a real leak: video_widget_ui.go's handleVideoFrame calls
// startMetalVideoOnWindow -> MetalVideoCreate again on frameNum==1 even when
// the Metal overlay from a PRIOR session is still active -- documented
// in-code as a deliberate "safe no-op/replace" for the case where a codec
// restart's new stream starts before the old one's stopMetalVideo() ran.
// metal_video_create's "replace" branch tore down the old NSView/CALayers
// but never invalidated the old CADisplayLink, which is kept alive by
// NSRunLoop (not by the g_display_link global, immediately overwritten) --
// each replace left one more CADisplayLink permanently firing
// displayLinkFired at full refresh rate, compounding render/diagnostic work
// and explaining a session that slowly accumulates CPU/memory use and
// stutter across multiple codec restarts, never at connect time.
//
// Uses a real NSWindow (see metal_video_testhelpers_darwin.go) since
// metal_video_create's only real precondition is a non-nil contentView --
// same pattern as cmd/metalspike/main.go's validated harness. Gated behind
// the `metaltest` build tag (cgo can't live in a _test.go file, and this
// creates a real AppKit window, unsuitable for a headless default `go test
// ./...`): run explicitly with
//
//	go test -tags metaltest ./internal/service/ -run TestMetalVideoCreateReplace -v
func TestMetalVideoCreateReplaceInvalidatesPriorDisplayLink(t *testing.T) {
	if !metalTestIsMainThread() {
		t.Skip("must run on the process main thread for AppKit dispatch_sync to not deadlock (see metalTestIsMainThread doc comment)")
	}

	win := metalTestMakeWindow()
	if win == 0 {
		t.Fatal("metalTestMakeWindow failed")
	}
	defer metalTestReleaseWindow(win)
	defer MetalVideoDestroy()

	if !MetalVideoCreate(win, 0, 0, 0, 0) {
		t.Fatal("first MetalVideoCreate failed")
	}
	createdAfter1, invalidatedAfter1 := MetalVideoDebugLinkCounts()
	if createdAfter1 == 0 {
		t.Fatal("expected at least one CADisplayLink to have been created")
	}

	// The buggy call: create again WITHOUT destroying first, exactly what
	// handleVideoFrame's frameNum==1 bootstrap does against an
	// already-active overlay.
	if !MetalVideoCreate(win, 0, 0, 0, 0) {
		t.Fatal("second (replace) MetalVideoCreate failed")
	}
	createdAfter2, invalidatedAfter2 := MetalVideoDebugLinkCounts()

	if createdAfter2 != createdAfter1+1 {
		t.Fatalf("created count = %d, want %d (one new CADisplayLink for the replace)", createdAfter2, createdAfter1+1)
	}
	if invalidatedAfter2 != invalidatedAfter1+1 {
		t.Fatalf("LEAK: invalidated count = %d, want %d -- the prior session's CADisplayLink was never invalidated and is still firing in the background", invalidatedAfter2, invalidatedAfter1+1)
	}
}
