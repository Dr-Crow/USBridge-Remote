//go:build !linux || android

package platform

import "fmt"

// ListMIDIInputs: MIDI input capture is only implemented on Linux so far.
func ListMIDIInputs() []MIDIInputInfo { return nil }

// StartMIDICapture: see ListMIDIInputs.
func StartMIDICapture(id string, onData func([]byte)) (UplinkCapture, error) {
	return nil, fmt.Errorf("MIDI input capture is not supported on this platform")
}
