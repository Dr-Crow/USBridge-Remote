//go:build linux

package streamhost

import (
	"bytes"
	"fmt"
	"os"
	"os/exec"
	"syscall"
	"testing"
	"time"
)

// A streamer started through usbridge-streamer-launch runs from a memfd,
// so its comm isn't its name and killall misses it (confirmed live);
// orphan cleanup has to find it by argv[0].
func TestKillOrphansByArgv0_FindsProcessByArgv0(t *testing.T) {
	const name = "usbridge-orphan-test-dummy"
	cmd := exec.Command("/bin/sleep", "30")
	cmd.Args[0] = name
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	done := make(chan struct{})
	go func() { _ = cmd.Wait(); close(done) }()
	t.Cleanup(func() { _ = cmd.Process.Kill(); <-done })
	waitForProcFixture(t, cmd.Process.Pid, name)

	killOrphansByArgv0([]string{name})
	select {
	case <-done:
	case <-time.After(3 * time.Second):
		cmd.Process.Kill()
		t.Fatal("process with matching argv[0] was not killed")
	}
}

func TestKillOrphansByArgv0_LeavesOthersAlone(t *testing.T) {
	cmd := exec.Command("/bin/sleep", "30")
	cmd.Args[0] = "usbridge-orphan-test-bystander"
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	defer func() { cmd.Process.Kill(); cmd.Wait() }()
	waitForProcFixture(t, cmd.Process.Pid, "usbridge-orphan-test-bystander")
	killOrphansByArgv0([]string{"usbridge-orphan-test-dummy"})
	time.Sleep(100 * time.Millisecond)
	if err := cmd.Process.Signal(syscall.Signal(0)); err != nil {
		t.Fatal("non-matching process was killed")
	}
}

// Start's exec handshake does not guarantee /proc's argv/ownership snapshot is
// already observable to the scanner. Wait for the fixture, not for the outcome
// under test. Production orphan detection and its kill deadline are unchanged.
func waitForProcFixture(t *testing.T, pid int, name string) {
	t.Helper()
	base := fmt.Sprintf("/proc/%d", pid)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		raw, readErr := os.ReadFile(base + "/cmdline")
		info, statErr := os.Stat(base)
		argv0, _, _ := bytes.Cut(raw, []byte{0})
		if readErr == nil && statErr == nil && string(argv0) == name {
			if st, ok := info.Sys().(*syscall.Stat_t); ok && st.Uid == uint32(os.Getuid()) {
				return
			}
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("child argv/ownership did not become visible in /proc")
}
