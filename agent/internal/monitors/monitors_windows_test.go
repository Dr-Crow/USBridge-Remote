//go:build windows

package monitors

import (
	"os"
	"strings"
	"testing"
)

// Runs against the machine's real monitors.
func TestListReportsTheRealMonitors(t *testing.T) {
	list, err := List()
	if err != nil {
		t.Skipf("no monitors (headless?): %v", err)
	}
	primaries := 0
	for _, m := range list {
		t.Logf("%+v", m)
		if !strings.HasPrefix(m.ID, `\\.\DISPLAY`) {
			t.Errorf("monitor ID %q is not a GDI device name", m.ID)
		}
		if m.Width <= 0 || m.Height <= 0 {
			t.Errorf("monitor %s has no size: %dx%d", m.ID, m.Width, m.Height)
		}
		if m.Primary {
			primaries++
		}
		if _, _, ok := LogicalOrigin(m.ID); !ok {
			t.Errorf("LogicalOrigin(%s) not found", m.ID)
		}
	}
	if primaries != 1 {
		t.Errorf("%d primary monitors, want exactly 1", primaries)
	}
	if !list[0].Primary {
		t.Errorf("primary monitor isn't listed first")
	}
}

func TestProcessMonitorIsFalseWithoutWindows(t *testing.T) {
	// This test binary has no windows.
	if id, ok := ProcessMonitor(os.Getpid()); ok {
		t.Fatalf("ProcessMonitor = %q for a process without windows", id)
	}
}
