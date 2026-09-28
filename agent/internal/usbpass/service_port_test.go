package usbpass

import (
	"net"
	"testing"
)

// pickURBPort must move off a configured port some unrelated process already
// holds (live case: Wondershare's WsToastNotification.exe on 8090) instead of
// letting the broker die on bind every watchdog tick.
func TestPickURBPortFallsBackWhenTaken(t *testing.T) {
	squatter, err := net.Listen("tcp4", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	defer squatter.Close()
	taken := squatter.Addr().(*net.TCPAddr).Port

	s := New("", t.TempDir(), "", taken, "")
	got := s.pickURBPort()
	if got == taken {
		t.Fatalf("pickURBPort returned the taken port %d", taken)
	}
	if got < taken || got > taken+urbPortFallbacks {
		t.Fatalf("pickURBPort = %d, want within (%d, %d]", got, taken, taken+urbPortFallbacks)
	}
}

func TestPickURBPortKeepsFreeConfiguredPort(t *testing.T) {
	ln, err := net.Listen("tcp", "0.0.0.0:0")
	if err != nil {
		t.Fatal(err)
	}
	free := ln.Addr().(*net.TCPAddr).Port
	ln.Close()

	s := New("", t.TempDir(), "", free, "")
	if got := s.pickURBPort(); got != free {
		t.Fatalf("pickURBPort = %d, want configured %d", got, free)
	}
}
