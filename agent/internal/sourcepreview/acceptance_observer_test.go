//go:build linux && source_preview_acceptance

package sourcepreview

import (
	"context"
	"encoding/json"
	"strings"
	"testing"

	"usbridge_agent/internal/localcomponents"
)

func TestAcceptanceObserverDoesNotGrantConsent(t *testing.T) {
	m, observe := NewAcceptanceObservedManager()
	_, err := m.Start(context.Background(), Approval{Components: localcomponents.Options{Directory: "/nonexistent", ManifestSHA256: strings.Repeat("0", 64)}, Display: ":96", FFmpeg: "/usr/bin/ffmpeg"})
	if err == nil {
		t.Fatal("missing approval accepted")
	}
	s := observe()
	if s.PrepareCalls != 0 || s.StreamStarts != 0 || s.ViewerStarts != 0 {
		t.Fatal("observer bypassed consent")
	}
	raw, err := json.Marshal(s)
	if err != nil {
		t.Fatal(err)
	}
	for _, secret := range []string{"key_b64", "session_id", "rtsp_url", "manifest", "ffmpeg"} {
		if strings.Contains(string(raw), secret) {
			t.Fatal("observer exported a descriptor field")
		}
	}
	if !s.FreshKeys || !s.FreshIDs || !s.FreshKeyIDs || !s.CleanJoins {
		t.Fatal("invalid initial observer")
	}
}
