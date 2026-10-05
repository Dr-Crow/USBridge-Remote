package usbpass

import (
	"net"
	"testing"
	"time"
)

// A broker that accepts the control connection and never answers must not
// hold Status forever: the GUI's refresh waits on it, so a hang here showed
// every permission as missing with dead Grant buttons.
func TestControlGivesUpOnASilentBroker(t *testing.T) {
	ln, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer ln.Close()
	held := make(chan net.Conn, 4)
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			held <- c // kept open, never read or answered
		}
	}()
	defer func() {
		for {
			select {
			case c := <-held:
				c.Close()
			default:
				return
			}
		}
	}()

	prev := controlTimeout
	controlTimeout = 300 * time.Millisecond
	defer func() { controlTimeout = prev }()

	s := &Service{controlAddr: ln.Addr().String()}
	done := make(chan error, 1)
	go func() {
		_, err := s.control("status", nil)
		done <- err
	}()
	select {
	case err := <-done:
		if err == nil {
			t.Fatal("control returned no error from a broker that never answered")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("control is still waiting on a broker that never answers")
	}
}
