//go:build windows && cgo

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

func TestSourcePreviewWindowsNativeWireProfile(t *testing.T) {
	for _, keyID := range []uint32{0, 1, 0xfedcba98, 0xffffffff} {
		got := windowsPreviewConfigurationProbe(keyID, true)
		want := [7]uint32{1056, 1, 0, 1, 0, 0, keyID}
		if got != want {
			t.Fatalf("preview profile = %v, want %v", got, want)
		}
	}
	got := windowsPreviewConfigurationProbe(1, false)
	if got[0] != 1200 || got[1] != 0 || got[2] != 1 || got[3] != 0 || got[4] != 1 || got[5] == 0 || got[6] != 1 {
		t.Fatalf("stock profile changed: %v", got)
	}
}

func TestSourcePreviewWindowsDecodesAudioWithoutWASAPI(t *testing.T) {
	// A genuine five-millisecond Opus encode/decode passes through ar_init,
	// ar_decode and ar_cleanup. Backend probes prevent any physical output if
	// a regression accidentally enters WASAPI and count each such entry.
	got := windowsPreviewAudioProbe()
	if got != [5]int{0, 240, 0, 0, 0} {
		t.Fatalf("native audio profile = %v, want [0 240 0 0 0]", got)
	}
}

func TestSourcePreviewWindowsViewOnlyCannotBecomeStock(t *testing.T) {
	w := &MoonlightCgoWrapper{host: "127.0.0.1", viewOnly: true}
	if err := w.StartStream("127.0.0.1:45678", make([]byte, 16), "7.1.431.-1", "", 1, 1, 128, 72, 30, 1000, nil, nil, nil); err == nil {
		t.Fatal("view-only wrapper admitted stock start")
	}
}

func TestSourcePreviewWindowsRejectsInvalidNativeProfile(t *testing.T) {
	for _, tc := range []struct {
		host                         string
		keyBytes, packetSize, format int
	}{
		{"remote.example", 16, 1056, 1},
		{"127.0.0.1", 0, 1056, 1},
		{"127.0.0.1", 15, 1056, 1},
		{"127.0.0.1", 17, 1056, 1},
		{"127.0.0.1", 16, 1200, 1},
		{"127.0.0.1", 16, 1056, 0x100},
	} {
		w := NewMoonlightCgoWrapper(tc.host)
		if err := w.startStream("rtspenc://127.0.0.1:45678", make([]byte, tc.keyBytes), "7.1.431.-1", "", 1, tc.format, 128, 72, 30, 1000, nil, nil, nil,
			moonlightNativeOptions{keyID: 0xfedcba98, packetSize: tc.packetSize, encryptedPreview: true}); err == nil {
			t.Fatal("invalid private profile reached native start")
		}
	}
}
