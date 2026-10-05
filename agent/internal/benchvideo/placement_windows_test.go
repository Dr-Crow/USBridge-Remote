//go:build windows

package benchvideo

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	"usbridge_agent/internal/monitors"
)

// Plays the benchmark video on every monitor of this machine with the real
// player and checks where its window really is. Opt-in: it takes over each
// screen for a few seconds. Run with USBRIDGE_BENCH_REAL=1 and, to skip the
// download, USBRIDGE_BENCH_VIDEO=<cached mp4>.
func TestPlayerOpensOnEachRealMonitor(t *testing.T) {
	if os.Getenv("USBRIDGE_BENCH_REAL") != "1" {
		t.Skip("set USBRIDGE_BENCH_REAL=1 to play on the real monitors")
	}
	list, err := monitors.List()
	if err != nil {
		t.Fatal(err)
	}
	if name, _ := findPlayer(); name == "" {
		t.Skip("no video player installed")
	}
	p := New(t.TempDir())
	defer p.Stop()
	for _, m := range list {
		m := m
		info, err := p.Start(context.Background(), &m, "")
		if err != nil {
			t.Fatalf("start on %s: %v", m.ID, err)
		}
		t.Logf("%s (%s): requested %s, window on %s", m.ID, m.Name, info.RequestedMonitor, info.Monitor)
		if info.Monitor != m.ID {
			t.Errorf("player asked for %s ended up on %q", m.ID, info.Monitor)
		}
		time.Sleep(2 * time.Second)
		p.Stop()
		time.Sleep(500 * time.Millisecond)
	}
}

// A player that ignored its placement and opened on the primary monitor
// gets moved onto the requested one.
func TestMisplacedPlayerIsMovedToTheRequestedMonitor(t *testing.T) {
	if os.Getenv("USBRIDGE_BENCH_REAL") != "1" {
		t.Skip("set USBRIDGE_BENCH_REAL=1 to play on the real monitors")
	}
	list, err := monitors.List()
	if err != nil || len(list) < 2 {
		t.Skip("needs two monitors")
	}
	player, path := findPlayer()
	if player != "ffplay" {
		t.Skip("needs ffplay")
	}
	content := os.Getenv("USBRIDGE_BENCH_VIDEO")
	args, _, err := playerArgs(player, content)
	if err != nil {
		t.Fatal(err)
	}
	proc, err := launch(path, args) // no placement: opens on the primary
	if err != nil {
		t.Fatal(err)
	}
	defer proc.Kill()
	target := list[1] // List puts the primary first
	time.Sleep(1500 * time.Millisecond)
	before, _ := monitors.ProcessMonitor(proc.Pid())
	got := ensureOnMonitor(proc.Pid(), target)
	t.Logf("opened on %s, asked for %s, now on %s", before, target.ID, got)
	if before == target.ID {
		t.Skipf("player already opened on %s; nothing to move", target.ID)
	}
	if got != target.ID {
		t.Fatalf("player on %q after the move, want %s", got, target.ID)
	}
	time.Sleep(2 * time.Second)
}

// Keeps the video playing on USBRIDGE_BENCH_HOLD_MONITOR for
// USBRIDGE_BENCH_HOLD_SECONDS, for checking a capture of that monitor by
// hand while it plays.
func TestHoldPlayerOnMonitor(t *testing.T) {
	id := os.Getenv("USBRIDGE_BENCH_HOLD_MONITOR") // DISPLAY6 or \\.\DISPLAY6
	if id == "" {
		t.Skip("set USBRIDGE_BENCH_HOLD_MONITOR")
	}
	if !strings.HasPrefix(id, `\\.\`) {
		id = `\\.\` + id
	}
	list, err := monitors.List()
	if err != nil {
		t.Fatal(err)
	}
	m, ok := monitors.Find(list, id)
	if !ok {
		t.Fatalf("no monitor %s", id)
	}
	secs, _ := time.ParseDuration(os.Getenv("USBRIDGE_BENCH_HOLD_SECONDS") + "s")
	p := New(t.TempDir())
	defer p.Stop()
	info, err := p.Start(context.Background(), &m, "")
	if err != nil {
		t.Fatal(err)
	}
	t.Logf("playing on %s", info.Monitor)
	time.Sleep(secs)
}
