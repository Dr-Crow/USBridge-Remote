//go:build linux && source_preview_engine_acceptance

package app

import (
	"testing"
	"usbridge_agent/internal/config"
)

func TestEnginePreviewMetadataIsBounded(t *testing.T) {
	// Never call App.New in a unit test: its real USBPass listener lives until
	// OS process exit. This only checks the observer's closed output schema.
	a := &App{cfg: config.Default(), usbPassBridgeAddr: "127.0.0.1:12345"}
	got := a.SourcePreviewEngineMetadata()
	if got["app_new_completed"] != false || got["usbpass_loopback"] != true || got["usbpass_port"] != 12345 {
		t.Fatal("incorrect bounded metadata")
	}
	for key, value := range got {
		switch value.(type) {
		case bool, int:
		default:
			t.Fatalf("metadata %s could export free text", key)
		}
	}
	a.usbPassBridgeAddr = "0.0.0.0:12345"
	if a.SourcePreviewEngineMetadata()["usbpass_loopback"] != false {
		t.Fatal("accepted non-loopback USBPass listener")
	}
}
