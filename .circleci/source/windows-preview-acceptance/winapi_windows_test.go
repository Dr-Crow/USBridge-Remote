//go:build windows

package main

import (
	"bufio"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

// Only the native test binary has this helper; the acceptance executable has no
// alternate arbitrary-execution mode. It never opens a window or captures media.
func TestWindowsPrivatePipeHelper(t *testing.T) {
	mode := os.Getenv("WINDOWS_PREVIEW_TEST_CHILD")
	if mode == "" {
		return
	}
	if mode == "leaf" {
		_, _ = io.Copy(io.Discard, os.Stdin)
		os.Exit(0)
	}
	reader := bufio.NewReader(os.Stdin)
	raw, e := reader.ReadString('\n')
	if e != nil || raw != "private-test-request\n" {
		os.Exit(2)
	}
	if mode == "pre-resume-exit" {
		inner, err := newJob()
		if err != nil {
			os.Exit(2)
		}
		exe, err := os.Executable()
		if err != nil {
			os.Exit(2)
		}
		inner.beforeResume = func(pid uint32) {
			fmt.Fprintf(os.Stdout, "%d\n", pid)
			ack, err := reader.ReadString('\n')
			if err != nil || ack != "exit-before-resume\n" {
				os.Exit(2)
			}
			// Deliberately bypass every defer. Windows must close the owning
			// job handle at process exit and retire the never-resumed child.
			os.Exit(0)
		}
		env := append(childEnvironment(os.Getenv("SystemRoot"), os.Getenv("TEMP")), "WINDOWS_PREVIEW_TEST_CHILD=leaf")
		_, _ = inner.start(exe, []string{"-test.run=^TestWindowsPrivatePipeHelper$"}, env, os.Getenv("TEMP"))
		os.Exit(2)
	}
	if mode == "spawn" {
		exe, e := os.Executable()
		if e != nil {
			os.Exit(2)
		}
		cmd := exec.Command(exe, "-test.run=^TestWindowsPrivatePipeHelper$")
		cmd.Env = append(childEnvironment(os.Getenv("SystemRoot"), os.Getenv("TEMP")), "WINDOWS_PREVIEW_TEST_CHILD=leaf")
		pipe, e := cmd.StdinPipe()
		if e != nil {
			os.Exit(2)
		}
		defer pipe.Close()
		if cmd.Start() != nil {
			os.Exit(2)
		}
		fmt.Fprintf(os.Stdout, "%d\n", cmd.Process.Pid)
	} else {
		fmt.Fprintln(os.Stdout, "private-test-reply")
	}
	_, _ = io.Copy(io.Discard, reader)
	os.Exit(0)
}
func TestWindowsAPILayouts(t *testing.T) {
	if unsafe.Sizeof(jobLimits{}) != 64 || unsafe.Sizeof(extendedJobLimits{}) != 144 || unsafe.Sizeof(bitmapHeader{}) != 40 || unsafe.Sizeof(startupInfoEx{}) != 112 || unsafe.Sizeof(jobProcessList{}) != 1032 || unsafe.Offsetof(jobProcessList{}.PIDs) != 8 {
		t.Fatal("Windows amd64 ABI layout changed")
	}
}
func testChild(t *testing.T, j *job, mode string) *child {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	work := t.TempDir()
	env := append(childEnvironment(os.Getenv("SystemRoot"), work), "WINDOWS_PREVIEW_TEST_CHILD="+mode)
	p, e := j.start(exe, []string{"-test.run=^TestWindowsPrivatePipeHelper$"}, env, work)
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { j.close(); _ = p.wait(3 * time.Second); p.close() })
	if !j.contains(p.handle) {
		t.Fatal("child not in exact job")
	}
	if _, e = p.stdin.Write([]byte("private-test-request\n")); e != nil {
		t.Fatal("private pipe write failed")
	}
	return p
}
func TestWindowsSuspendedLaunchPrivatePipesNaturalEOF(t *testing.T) {
	j, e := newJob()
	if e != nil {
		t.Fatal(e)
	}
	defer j.close()
	p := testChild(t, j, "echo")
	raw, e := p.next(3 * time.Second)
	if e != nil || string(raw) != "private-test-reply\n" {
		t.Fatal("private output missing")
	}
	pids, e := j.pids()
	if e != nil || len(pids) != 1 || pids[0] != p.pid {
		logInventoryQuery(t, j)
		t.Fatal("job ownership inventory mismatch")
	}
	p.stdin.Close()
	if p.wait(3*time.Second) != nil || p.finishProtocol() != nil {
		t.Fatal("natural child cleanup failed")
	}
	pids, e = j.pids()
	if e != nil || len(pids) != 0 {
		t.Fatal("job did not empty naturally")
	}
}
func TestWindowsJobCloseKillsInheritedDescendant(t *testing.T) {
	j, e := newJob()
	if e != nil {
		t.Fatal(e)
	}
	defer j.close()
	p := testChild(t, j, "spawn")
	raw, e := p.next(3 * time.Second)
	if e != nil {
		t.Fatal(e)
	}
	pid64, e := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 32)
	if e != nil {
		t.Fatal("child PID unavailable")
	}
	_, h, e := processPath(uint32(pid64))
	if e != nil {
		t.Fatal(e)
	}
	defer syscall.CloseHandle(h)
	if !j.contains(h) {
		t.Fatal("descendant did not inherit exact job")
	}
	pids, e := j.pids()
	if e != nil || len(pids) != 2 {
		logInventoryQuery(t, j)
		t.Fatal("descendant inventory mismatch")
	}
	j.close()
	select {
	case <-p.done:
		if p.exitCode == 0 {
			t.Fatal("safety termination masqueraded as clean exit")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owned direct child survived job closure")
	}
	result, e := syscall.WaitForSingleObject(h, 3000)
	if e != nil || result != syscall.WAIT_OBJECT_0 {
		t.Fatal("owned descendant survived job closure")
	}
}

func TestWindowsParentCrashRetiresSuspendedChild(t *testing.T) {
	outer, err := newJob()
	if err != nil {
		t.Fatal(err)
	}
	defer outer.close()
	parent := testChild(t, outer, "pre-resume-exit")
	raw, err := parent.next(3 * time.Second)
	if err != nil {
		t.Fatal(err)
	}
	pid, err := strconv.ParseUint(strings.TrimSpace(string(raw)), 10, 32)
	if err != nil {
		t.Fatal("suspended child PID missing")
	}
	_, suspended, err := processPath(uint32(pid))
	if err != nil {
		t.Fatal(err)
	}
	defer syscall.CloseHandle(suspended)
	if !outer.contains(suspended) {
		t.Fatal("suspended child did not inherit outer containment")
	}
	if state, err := syscall.WaitForSingleObject(suspended, 0); err != nil || state != syscall.WAIT_TIMEOUT {
		t.Fatal("suspended child was not alive before parent exit")
	}
	if _, err := parent.stdin.Write([]byte("exit-before-resume\n")); err != nil {
		t.Fatal(err)
	}
	if parent.wait(3*time.Second) != nil || parent.finishProtocol() != nil {
		t.Fatal("parent exit failed")
	}
	if state, err := syscall.WaitForSingleObject(suspended, 3000); err != nil || state != syscall.WAIT_OBJECT_0 {
		t.Fatal("pre-resume parent crash stranded a child")
	}
	pids, err := outer.pids()
	if err != nil || len(pids) != 0 {
		logInventoryQuery(t, outer)
		t.Fatal("outer safety job was needed to clean up")
	}
}

// Query-only diagnostics. Never print native PIDs, handles, paths or child data.
// Compare the documented optional return-length forms without changing pids().
func logInventoryQuery(t *testing.T, j *job) {
	t.Helper()
	for _, withLength := range []bool{false, true} {
		var list jobProcessList
		var returned uint32
		flag := 0
		var ok uintptr
		var nativeErr error
		if withLength {
			flag = 1
			ok, _, nativeErr = queryJob.Call(uintptr(j.handle), 3, uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list), uintptr(unsafe.Pointer(&returned)))
		} else {
			ok, _, nativeErr = queryJob.Call(uintptr(j.handle), 3, uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list), 0)
		}
		runtime.KeepAlive(&returned)
		var code uint32
		if ok == 0 {
			if errno, valid := nativeErr.(syscall.Errno); valid {
				code = uint32(errno)
			}
		}
		success := 0
		if ok != 0 {
			success = 1
		}
		t.Logf("inventory_probe return_pointer=%d ok=%d error=%d assigned=%d count=%d bytes=%d", flag, success, code, list.Assigned, list.Count, returned)
	}
}
