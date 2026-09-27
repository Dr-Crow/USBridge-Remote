//go:build windows

package streamhost

import "testing"

// Rows as usbridge-streamer --list-capture-devices really prints them
// (a laptop panel on an AMD iGPU plus a monitor on an NVIDIA dGPU).
func TestRustshineDeviceRowsYieldTheGDIName(t *testing.T) {
	rows := map[string]string{
		`0      \\.\DISPLAY1       AMD Radeon 780M Graphics     0x1002   2560x1600`: `\\.\DISPLAY1`,
		`1      \\.\DISPLAY6       NVIDIA GeForce RTX 3090      0x10DE   2560x1600`: `\\.\DISPLAY6`,
	}
	for row, want := range rows {
		m := rustshineDeviceLineRe.FindStringSubmatch(row)
		if m == nil {
			t.Fatalf("row not matched: %q", row)
		}
		if got := rustshineGDIName(m[2]); got != want {
			t.Errorf("GDI name of %q = %q, want %q", row, got, want)
		}
	}
	if got := rustshineGDIName("  "); got != "" {
		t.Errorf("GDI name of a blank field = %q, want empty", got)
	}
}
