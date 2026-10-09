//go:build !linux || android || !cgo

package platform

import "testing"

func TestUnsupportedCameraUplink(t *testing.T) {
	if CameraSupported() {
		t.Fatal("stub advertised camera support")
	}
	if devices := ListCameras(); len(devices) != 0 {
		t.Fatal("stub returned camera devices")
	}
	called := false
	capture, err := StartCameraCapture("test-only", func([]byte, bool) bool { called = true; return true })
	if err == nil || capture != nil || called {
		t.Fatal("unsupported capture must fail without calling input callback")
	}
}
