package app

import (
	"testing"
	"time"
)

// A backend switch holds streamMu for the new backend's whole startup;
// the GUI's status poll must still get an answer meanwhile.
func TestCurrentStreamKindDoesNotWaitForABackendSwitch(t *testing.T) {
	a := &App{}
	a.setStreamKind("sunshine")

	a.streamMu.Lock() // a switch in progress
	defer a.streamMu.Unlock()
	a.setStreamKind("rustshine")

	got := make(chan string, 1)
	go func() { got <- a.currentStreamKind() }()
	select {
	case kind := <-got:
		if kind != "rustshine" {
			t.Fatalf("currentStreamKind = %q, want the backend being started", kind)
		}
	case <-time.After(time.Second):
		t.Fatal("currentStreamKind blocked on a backend switch")
	}
}
