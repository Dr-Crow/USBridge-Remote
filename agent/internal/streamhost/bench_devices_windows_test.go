//go:build windows

package streamhost

import (
	"os"
	"testing"

	"usbridge_agent/internal/monitors"
)

// Checks, against this machine's installed streamers, that every real
// monitor resolves to a capture target in both of them -- what the
// benchmark's monitor pin relies on. Opt-in: set USBRIDGE_REAL_STATE_DIR to
// the agent's state dir (e.g. %APPDATA%\usbridge-agent).
func TestEveryRealMonitorMapsToBothStreamers(t *testing.T) {
	state := os.Getenv("USBRIDGE_REAL_STATE_DIR")
	if state == "" {
		t.Skip("set USBRIDGE_REAL_STATE_DIR to check the installed streamers")
	}
	list, err := monitors.List()
	if err != nil {
		t.Fatal(err)
	}
	for name, b := range map[string]Backend{
		"rustshine": NewRustshine(t.TempDir(), state, ""),
		"sunshine":  NewSunshine(t.TempDir(), state, ""),
	} {
		devices := b.ListCaptureDevices()
		if len(devices) == 0 {
			t.Errorf("%s reports no capture devices", name)
			continue
		}
		for _, m := range list {
			found := false
			for _, d := range devices {
				if d.GDIName == m.ID {
					t.Logf("%s: %s -> output %q (%s %dx%d)", name, m.ID, d.OutputName, d.DisplayName, d.Width, d.Height)
					found = true
				}
			}
			if !found {
				t.Errorf("%s has no capture device for monitor %s", name, m.ID)
			}
		}
	}
}
