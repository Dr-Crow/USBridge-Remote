//go:build linux

package benchvideo

import (
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

// Live check against a real KWin session with a Punktfunk virtual display
// up: opens a small test-pattern window and expects it moved onto that
// display. Opt-in (USBRIDGE_BENCH_LIVE_OUTPUT=<output name prefix>): it
// needs a desktop session and briefly puts a window on screen.
func TestMoveToOutput_Live(t *testing.T) {
	prefix := os.Getenv("USBRIDGE_BENCH_LIVE_OUTPUT")
	if prefix == "" {
		t.Skip("set USBRIDGE_BENCH_LIVE_OUTPUT to an output name prefix to run")
	}
	ffplay, err := exec.LookPath("ffplay")
	if err != nil {
		t.Skip("ffplay not installed")
	}
	// Fullscreen, like the benchmark's own player: that is the case where a
	// window keeps the old output's geometry unless fullscreen is re-applied.
	cmd := exec.Command(ffplay, "-hide_banner", "-loglevel", "error", "-an", "-fs", "-f", "lavfi", "-i", "testsrc2=size=640x360:rate=30")
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { _ = cmd.Process.Kill(); _ = cmd.Wait() }()

	name, err := moveToOutput(cmd.Process.Pid, prefix, 8*time.Second)
	if err != nil {
		t.Fatalf("moveToOutput: %v", err)
	}
	if !strings.HasPrefix(name, prefix) {
		t.Fatalf("moved to %q, want an output starting with %q", name, prefix)
	}
	t.Logf("window moved to %s", name)

	if _, err := moveToOutput(cmd.Process.Pid, "No-Such-Output", time.Second); err == nil {
		t.Fatal("moving to an output that doesn't exist reported success")
	}
}
