package api

import (
	"sync/atomic"
	"time"
)

// Live-frame bridge for local ui.parse (see tryLocalUIParse in
// local_ui_intercept.go): lets the video decode path
// (internal/service/ai_vision.go, which already imports this package for
// GetLocalUIParser/SetLocalUIParser) hand over its next decoded frame on
// demand, so a local ui.parse call made while a video session is already
// streaming doesn't ALSO pay for a device screenshot round-trip
// (screen.get_image over /api/mcp, plus whatever capture cost the device
// side pays for it) when a frame is already sitting locally, decoded,
// waiting to be displayed.
//
// This lives in package api, not service, specifically to avoid an import
// cycle: service already imports api (for GetLocalUIParser), so the
// dependency has to run this direction.
//
// Deliberately independent of the AI Vision overlay checkbox
// (service.SetAIVisionEnabled) -- that toggle gates a periodic (every
// aiVisionInterval) background detection pass burned into the live
// picture, a completely different concern from "give me whatever frame
// you're decoding right now, once". The video decode hot path (see
// ai_vision.go's ApplyAIVisionOverlay/goAIVisionSample) checks
// LiveFrameWanted on every frame regardless of that checkbox, at the cost
// of one atomic load when nobody's asking.
var (
	liveFrameWanted atomic.Bool
	liveFrameCh     = make(chan []byte, 1)

	// liveFrameGetter, when set (service package's init on platforms that
	// keep a stable "last rendered" buffer -- currently macOS's Metal path,
	// see metal_video_darwin.go's g_lastRenderedBuf), answers RequestLiveFrame
	// immediately from that already-composited frame instead of arming
	// liveFrameWanted and waiting for the decode thread's next callback.
	// Confirmed live: the wait-based path could catch a frame mid-reconnect
	// glitch (a visibly smeared/torn frame the operator never actually saw
	// as a stable picture) simply because it happened to be whatever the
	// decoder produced in the timeout window right after being asked --
	// asking "what's already on screen" instead of "decode me a fresh one"
	// avoids that class of bug entirely, not just the timing race the
	// timeout value alone fixed. nil on platforms without such an accessor,
	// where the wait-based fallback below still applies.
	liveFrameGetter atomic.Pointer[func() []byte]
)

// SetLiveFrameGetter registers a getter that returns the current frame
// (PNG-encoded) instantly, no waiting -- see liveFrameGetter's doc comment.
// Pass nil to clear it back to the wait-based fallback.
func SetLiveFrameGetter(fn func() []byte) {
	if fn == nil {
		liveFrameGetter.Store(nil)
		return
	}
	liveFrameGetter.Store(&fn)
}

// RequestLiveFrame asks the video decode path for a frame (PNG-encoded,
// full resolution). Prefers liveFrameGetter's instant "already rendered"
// snapshot when one is registered; otherwise waits up to timeout for the
// decode thread's next callback to hand one over via SubmitLiveFrame.
// Returns (nil, false) if no video session is actively decoding frames
// right now -- the caller should fall back to fetching a screenshot from
// the device in that case, exactly as if this didn't exist.
func RequestLiveFrame(timeout time.Duration) ([]byte, bool) {
	if p := liveFrameGetter.Load(); p != nil {
		if png := (*p)(); png != nil {
			return png, true
		}
		return nil, false
	}

	// Drain a stale frame left over from a previous call that timed out
	// before anything arrived, so this call doesn't get handed a frame
	// from seconds ago instead of a fresh one.
	select {
	case <-liveFrameCh:
	default:
	}

	liveFrameWanted.Store(true)
	defer liveFrameWanted.Store(false)

	select {
	case png := <-liveFrameCh:
		return png, true
	case <-time.After(timeout):
		return nil, false
	}
}

// LiveFrameWanted reports whether a RequestLiveFrame call is currently
// blocked waiting for a frame -- checked by the decode path's hot-path
// callback (internal/service/ai_vision.go) so it knows whether to bother
// capturing+encoding this frame, without service needing to import api (it
// already does) or api needing to import service (which would cycle).
func LiveFrameWanted() bool {
	return liveFrameWanted.Load()
}

// SubmitLiveFrame delivers one PNG-encoded frame to a pending
// RequestLiveFrame call. Non-blocking and a no-op if nobody's currently
// waiting (LiveFrameWanted was false, or another frame already claimed
// this request) -- the decode thread must never stall on this.
func SubmitLiveFrame(png []byte) {
	if !liveFrameWanted.CompareAndSwap(true, false) {
		return
	}
	select {
	case liveFrameCh <- png:
	default:
	}
}
