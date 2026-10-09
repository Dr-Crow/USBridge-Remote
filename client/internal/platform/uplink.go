package platform

// A local MIDI input or microphone that the client plays on the streaming
// host (USBridge extension, see service.MoonlightUplinkSender).

// MIDIInputInfo is one local MIDI input.
type MIDIInputInfo struct {
	ID   string // stable while the device stays plugged in
	Name string
}

// UplinkCapture is a running MIDI or microphone capture.
type UplinkCapture interface {
	Stop()
}

// MicFrameSamples is one microphone frame: 20ms at 48kHz mono, what
// StartMicCapture hands over Opus-encoded.
const MicFrameSamples = 960
