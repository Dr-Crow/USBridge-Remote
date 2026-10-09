//go:build linux && !android

package platform

import (
	"os"
	"testing"
	"time"
)

// TestMIDICaptureLive reads what USBRIDGE_TEST_MIDI_ID (a ListMIDIInputs id)
// plays for a few seconds; skipped without it.
func TestMIDICaptureLive(t *testing.T) {
	for _, in := range ListMIDIInputs() {
		t.Logf("MIDI input %q: %s", in.ID, in.Name)
	}
	id := os.Getenv("USBRIDGE_TEST_MIDI_ID")
	if id == "" {
		t.Skip("USBRIDGE_TEST_MIDI_ID not set")
	}
	got := make(chan []byte, 16)
	c, err := StartMIDICapture(id, func(b []byte) { got <- b })
	if err != nil {
		t.Fatal(err)
	}
	defer c.Stop()
	select {
	case b := <-got:
		t.Logf("received % x", b)
	case <-time.After(5 * time.Second):
		t.Fatal("nothing received")
	}
}
