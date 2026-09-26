package api

import (
	"log"
	"sync"
	"sync/atomic"
	"time"
)

// Input().AbsoluteEvent latches its button mask across calls, so a mouse
// button pressed by a client that then vanishes stays held on the host --
// a "stuck" mouse that drags everything until some later event releases
// it. mouseHold tracks the one absolute-mode press currently held (there
// is a single host pointer) and who owns it, so it can be released when:
//   - the owning mouse_ws closes,
//   - the owning mouse_ws goes silent (see mouseWSHeldStale) even though
//     its TCP connection hasn't been torn down yet,
//   - a new mouse_ws connects (a reconnect: nothing held carries over),
//   - an HTTP /api/mouse press isn't followed up (mouseHTTPHeldTimeout).
type mouseHold struct {
	mu      sync.Mutex
	held    bool
	owner   uint64
	x, y    int
	updated time.Time
}

var mouseOwnerSeq atomic.Uint64

// nextMouseOwner returns a unique owner id for one mouse_ws connection or
// one HTTP mouse request.
func nextMouseOwner() uint64 { return mouseOwnerSeq.Add(1) }

const (
	// While a button is held over mouse_ws the server pings every
	// mouseWSHeldPing and gives up on the socket once nothing at all
	// (message or pong) has arrived for mouseWSHeldStale -- a live client
	// holding a drag still answers pings, a dead one can't.
	mouseWSHeldPing  = time.Second
	mouseWSHeldStale = 4 * time.Second
	// HTTP presses have no connection to watch: release if the same press
	// isn't updated (moved/released) within this long.
	mouseHTTPHeldTimeout = 5 * time.Second
)

func isAbsoluteMouseAction(action string) bool {
	switch action {
	case "touch", "touch_position", "absolute_event":
		return true
	}
	return false
}

// noteMouse records the held state after an absolute mouse request from
// owner. Returns true when a button is now held.
func (s *Server) noteMouse(owner uint64, req MouseRequest) bool {
	if !isAbsoluteMouseAction(req.Action) {
		return false
	}
	h := &s.mouseHold
	h.mu.Lock()
	defer h.mu.Unlock()
	if ptrUint8(req.ButtonState) == 0 {
		h.held = false
		return false
	}
	h.held = true
	h.owner = owner
	h.x, h.y = ptrInt(req.X), ptrInt(req.Y)
	h.updated = time.Now()
	return true
}

// mouseHeldBy reports whether owner currently holds a button.
func (s *Server) mouseHeldBy(owner uint64) bool {
	h := &s.mouseHold
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.held && h.owner == owner
}

// releaseHeldMouse releases any held button at its last known position.
// owner == 0 releases regardless of who holds it; otherwise only owner's
// hold is released (a closing old socket must not release a press made by
// the connection that replaced it).
func (s *Server) releaseHeldMouse(owner uint64, reason string) {
	h := &s.mouseHold
	h.mu.Lock()
	if !h.held || (owner != 0 && h.owner != owner) {
		h.mu.Unlock()
		return
	}
	h.held = false
	x, y := h.x, h.y
	h.mu.Unlock()
	log.Printf("[api] releasing held mouse buttons (%s)", reason)
	if err := s.app.Input().AbsoluteEvent(0, uint16(x), uint16(y), 0); err != nil {
		log.Printf("[api] release held mouse buttons failed: %v", err)
	}
}

// releaseHTTPMouseLater releases owner's HTTP press if nothing updated it
// within mouseHTTPHeldTimeout.
func (s *Server) releaseHTTPMouseLater(owner uint64) {
	time.AfterFunc(mouseHTTPHeldTimeout, func() {
		h := &s.mouseHold
		h.mu.Lock()
		stale := h.held && h.owner == owner && time.Since(h.updated) >= mouseHTTPHeldTimeout
		h.mu.Unlock()
		if stale {
			s.releaseHeldMouse(owner, "http press not followed up")
		}
	})
}
