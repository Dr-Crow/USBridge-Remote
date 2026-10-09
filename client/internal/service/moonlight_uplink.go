package service

// MoonlightUplinkSender sends a local MIDI input, microphone and camera to a USBridge
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
	// CameraUplinkSupported reports whether the host shows this client's
	// camera (LiSendCameraFrame); false without a stream.
	CameraUplinkSupported() bool
	// SendMoonlightCamera queues one H.264 access unit of the camera; false
	// when it didn't go out (no stream, or the network is behind) -- the
	// next one should then be a keyframe.
	SendMoonlightCamera(frame uint16, keyframe bool, au []byte) bool
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

func (m *MoonlightService) CameraUplinkSupported() bool {
	if w := m.activeWrapper; w != nil {
		return w.CameraUplinkSupported()
	}
	return false
}

func (m *MoonlightService) SendMoonlightCamera(frame uint16, keyframe bool, au []byte) bool {
	if w := m.activeWrapper; w != nil {
		return w.SendMoonlightCamera(frame, keyframe, au)
	}
	return false
}
