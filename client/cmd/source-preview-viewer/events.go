package main

import (
	"encoding/json"
	"sync"
	"usbridge-client/internal/sourcepreview"
)

type events struct {
	mu                                sync.Mutex
	out                               *json.Encoder
	sessionID                         string
	ready, frame, firstSent, terminal bool
}

func (e *events) write(kind, reason string) {
	// Fixed fields and validated session identifiers only. Do not add errors,
	// descriptors, URLs or credentials here.
	_ = e.out.Encode(struct {
		SchemaVersion int    `json:"schema_version"`
		Event         string `json:"event"`
		SessionID     string `json:"session_id"`
		Reason        string `json:"reason,omitempty"`
	}{sourcepreview.SchemaVersion, kind, e.sessionID, reason})
}
func (e *events) connected() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.ready || e.terminal {
		return
	}
	e.ready = true
	e.write("ready", "")
	if e.frame && !e.firstSent {
		e.firstSent = true
		e.write("first_frame", "")
	}
}
func (e *events) displayed() {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.terminal {
		return
	}
	e.frame = true
	if e.ready && !e.firstSent {
		e.firstSent = true
		e.write("first_frame", "")
	}
}
func (e *events) stopped(reason string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.terminal {
		return
	}
	e.terminal = true
	if reason == "renderer_error" || reason == "display_unavailable" || reason == "lease_protocol_error" {
		reason = "failed"
	} else {
		reason = "completed"
	}
	e.write("stopped", reason)
}

// Optional closed failure code is emitted only before readiness. Older strict
// supervisors reject this failed startup safely; successful lifecycle JSON is
// unchanged. Never serialize a native error or descriptor here.
func (e *events) startupFailed(code string) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.terminal || e.ready {
		return
	}
	if !validWindowFailure(code) {
		code = "window_unavailable"
	}
	e.terminal = true
	_ = e.out.Encode(struct {
		SchemaVersion int    `json:"schema_version"`
		Event         string `json:"event"`
		SessionID     string `json:"session_id"`
		Reason        string `json:"reason"`
		FailureCode   string `json:"failure_code"`
	}{sourcepreview.SchemaVersion, "stopped", e.sessionID, "failed", code})
}
