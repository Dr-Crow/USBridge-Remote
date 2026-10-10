//go:build linux && !android && cgo

package service

import "testing"

func TestSourcePreviewNativeRejectsPlaintextBeforeStart(t *testing.T) {
	w := NewMoonlightCgoWrapper("127.0.0.1")
	if err := w.startStream("rtsp://127.0.0.1:45678", make([]byte, 16), "7.1.431.-1", "", 1, 1, 640, 360, 30, 1000, nil, nil, nil,
		moonlightNativeOptions{keyID: 0xfedcba98, packetSize: 1056, encryptedPreview: true}); err == nil {
		t.Fatal("source plaintext accepted")
	}
	if err := w.startStream("rtspenc://127.0.0.1:45678", make([]byte, 16), "7.1.431.-1", "", 1, 1, 640, 360, 30, 1000, nil, nil, nil,
		moonlightNativeOptions{keyID: 0xfedcba98, packetSize: 1024, encryptedPreview: true}); err == nil {
		t.Fatal("unmapped packet budget accepted")
	}
}

func TestSourcePreviewNativeBlocksAllInputAndUplinks(t *testing.T) {
	was := liStartConnectionActive.Swap(true)
	defer liStartConnectionActive.Store(was)
	w := &MoonlightCgoWrapper{viewOnly: true}
	if w.IsInputActive() || w.RawHIDEpoch() != 0 || w.CameraUplinkSupported() {
		t.Fatal("view-only advertised input")
	}
	if midi, mic := w.UplinkSupport(); midi || mic {
		t.Fatal("view-only advertised uplinks")
	}
	w.SendMoonlightKey(1, 1, 0)
	w.SendMoonlightMouseMove(1, 1)
	w.SendMoonlightMousePosition(1, 1, 640, 360)
	w.SendMoonlightMouseButton(1, 1)
	w.SendMoonlightScroll(1)
	w.SendMoonlightUtf8Text("test")
	w.SendMoonlightControllerEvent(0, 1, 0, 0, 0, 0, 0, 0, 0)
	w.SendMoonlightControllerArrival(0, 1, 0, 0, 0)
	w.SendMoonlightPenEvent(0, 0, 0, 0, 0, 0, 0, 0)
	if w.SendMoonlightMIDI([]byte{1}) || w.SendMoonlightMic(1, []byte{1}) || w.SendMoonlightCamera(1, true, []byte{1}) || w.SendMoonlightRawHID(0, 0, 0, 1, 0, []byte{1}, true) {
		t.Fatal("view-only sent uplink")
	}
}
