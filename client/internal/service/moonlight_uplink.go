package service

// MoonlightUplinkSender sends a local MIDI input and microphone to a USBridge
// host, over the stream's control channel (LiSendMidiEvent/LiSendMicAudio):
// the host plays them out of its own devices -- on a NanoKVM a USB MIDI port
// and the USB microphone of the target PC. Kept apart from
// MoonlightInputSender: only a native Moonlight stream can do this.
type MoonlightUplinkSender interface {
	// UplinkSupport reports what the connected host takes; both false
	// without a stream.
	UplinkSupport() (midi, mic bool)
	// SendMoonlightMIDI queues whole MIDI messages (at most 128 bytes).
	SendMoonlightMIDI(data []byte) bool
	// SendMoonlightMic queues one Opus frame (48kHz mono, at most 200 bytes).
	SendMoonlightMic(sequence uint16, opus []byte) bool
}

func (m *MoonlightService) UplinkSupport() (midi, mic bool) {
	if w := m.activeWrapper; w != nil {
		return w.UplinkSupport()
	}
	return false, false
}

func (m *MoonlightService) SendMoonlightMIDI(data []byte) bool {
	if w := m.activeWrapper; w != nil {
		return w.SendMoonlightMIDI(data)
	}
	return false
}

func (m *MoonlightService) SendMoonlightMic(sequence uint16, opus []byte) bool {
	if w := m.activeWrapper; w != nil {
		return w.SendMoonlightMic(sequence, opus)
	}
	return false
}
