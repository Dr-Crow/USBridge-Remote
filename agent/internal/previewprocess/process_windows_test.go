//go:build windows

package previewprocess

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

const helperKey = "PREVIEWPROCESS_SYNTHETIC_HELPER"

func helperEnvironment(mode, dir string) []string {
	return []string{"SystemRoot=" + os.Getenv("SystemRoot"), "TEMP=" + dir, "TMP=" + dir, helperKey + "=" + mode}
}
func helperSpec(t *testing.T, mode string) Spec {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal("test executable unavailable")
	}
	dir := t.TempDir()
	return Spec{Path: exe, Args: []string{"-test.run=^TestWindowsSyntheticHelper$"}, Dir: dir, Env: helperEnvironment(mode, dir)}
}

// The helper exists only in the native test binary. All descendants are that
// same synthetic binary. No GUI, capture, input, desktop, or media API is used.
func TestWindowsSyntheticHelper(t *testing.T) {
	mode := os.Getenv(helperKey)
	if mode == "" {
		return
	}
	if mode == "leaf" {
		for {
			time.Sleep(time.Hour)
		}
	}
	if mode == "unread" {
		fmt.Fprintln(os.Stdout, "ready")
		for {
			time.Sleep(time.Hour)
		}
	}
	reader := bufio.NewReader(os.Stdin)
	request, e := reader.ReadString('\n')
	if e != nil || request != "private-request\n" {
		os.Exit(2)
	}
	if mode == "owner-before-resume" {
		exe, e := os.Executable()
		if e != nil {
			os.Exit(2)
		}
		s := Spec{Path: exe, Args: []string{"-test.run=^TestWindowsSyntheticHelper$"}, Dir: os.Getenv("TEMP"), Env: helperEnvironment("leaf", os.Getenv("TEMP"))}
		_, _ = startWithHook(context.Background(), s, func(pid uint32, _ *nativeJob) {
			fmt.Fprintf(os.Stdout, "%d\n", pid)
			ack, e := reader.ReadString('\n')
			if e != nil || ack != "exit-before-resume\n" {
				os.Exit(2)
			}
			os.Exit(0) // Deliberately bypass every defer and package cleanup path.
		})
		os.Exit(2)
	}
	if mode == "spawn" {
		exe, e := os.Executable()
		if e != nil {
			os.Exit(2)
		}
		cmd := exec.Command(exe, "-test.run=^TestWindowsSyntheticHelper$")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x8}
		cmd.Env = helperEnvironment("leaf", os.Getenv("TEMP"))
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr // Leaf deliberately keeps output open.
		if cmd.Start() != nil {
			os.Exit(2)
		}
		fmt.Fprintf(os.Stdout, "%d\n", cmd.Process.Pid)
		command, e := reader.ReadString('\n')
		if e != nil {
			os.Exit(0)
		}
		if command == "crash\n" {
			os.Exit(7)
		}
		if command == "zero\n" {
			os.Exit(0)
		}
		os.Exit(2)
	}
	if mode == "probe" {
		h, e := strconv.ParseUint(os.Getenv("PREVIEWPROCESS_SENTINEL"), 10, 64)
		if e != nil {
			os.Exit(2)
		}
		// If an unknown inheritable event leaked, this signals the observer's event.
		kernel32.NewProc("SetEvent").Call(uintptr(h))
	}
	fmt.Fprintln(os.Stdout, "private-reply")
	fmt.Fprintln(os.Stderr, "private-stderr")
	_, _ = io.Copy(io.Discard, reader)
	os.Exit(0)
}
func nativeProcess(t *testing.T, ctx context.Context, mode string) *Process {
	t.Helper()
	p, e := Start(ctx, helperSpec(t, mode))
	if e != nil {
		t.Fatal("native start failed")
	}
	t.Cleanup(func() { _, _ = p.Stop(0); _ = p.Stdin.Close(); _ = p.Stdout.Close(); _ = p.Stderr.Close() })
	return p
}
func lineWithin(t *testing.T, r io.Reader) string {
	t.Helper()
	type answer struct {
		line string
		e    error
	}
	ch := make(chan answer, 1)
	go func() { line, e := bufio.NewReader(r).ReadString('\n'); ch <- answer{line, e} }()
	select {
	case a := <-ch:
		if a.e != nil {
			t.Fatal("private response failed")
		}
		return a.line
	case <-time.After(3 * time.Second):
		t.Fatal("private response timed out")
		return ""
	}
}
func writeRequest(t *testing.T, p *Process) {
	t.Helper()
	if _, e := io.WriteString(p.Stdin, "private-request\n"); e != nil {
		t.Fatal("private request failed")
	}
}
func processHandle(t *testing.T, pid uint32) syscall.Handle {
	t.Helper()
	h, e := syscall.OpenProcess(syscall.SYNCHRONIZE|0x1000|syscall.PROCESS_TERMINATE, false, pid)
	if e != nil {
		t.Fatal("owned process handle unavailable")
	}
	t.Cleanup(func() { _ = syscall.CloseHandle(h) })
	return h
}
func requireSignal(t *testing.T, h syscall.Handle) {
	t.Helper()
	state, e := syscall.WaitForSingleObject(h, 3000)
	if e != nil || state != syscall.WAIT_OBJECT_0 {
		t.Fatal("owned process did not exit")
	}
}
func requireInventory(t *testing.T, p *Process, expected ...uint32) {
	t.Helper()
	ids, e := p.owner.inventory()
	if e != nil || len(ids) != len(expected) {
		t.Fatal("exact job inventory mismatch")
	}
	want := make(map[uint32]bool)
	for _, id := range expected {
		want[id] = true
	}
	for _, id := range ids {
		if !want[id] {
			t.Fatal("unexpected job member")
		}
	}
}
func requireNoninheritable(t *testing.T, h syscall.Handle) {
	t.Helper()
	var flags uint32
	ok, _, _ := kernel32.NewProc("GetHandleInformation").Call(uintptr(h), uintptr(unsafe.Pointer(&flags)))
	if ok == 0 || flags&syscall.HANDLE_FLAG_INHERIT != 0 {
		t.Fatal("owner handle is inheritable")
	}
}
func TestWindowsNativeLayoutsAndEmptyEnvironment(t *testing.T) {
	if unsafe.Sizeof(jobLimits{}) != 64 || unsafe.Sizeof(extendedJobLimits{}) != 144 || unsafe.Sizeof(startupInfoEx{}) != 112 || unsafe.Sizeof(jobProcessList{}) != 1032 {
		t.Fatal("64-bit Windows ABI mismatch")
	}
	env, e := environmentBlock(nil)
	if e != nil || len(env) != 2 || env[0] != 0 || env[1] != 0 {
		t.Fatal("empty environment is not double terminated")
	}
	if launchFlags&0x8 == 0 || launchFlags&0x08000000 != 0 || launchFlags&0x01000000 != 0 {
		t.Fatal("detached/no-breakaway flags changed")
	}
}
func TestWindowsPrivatePipesExactOwnershipAndNaturalEOF(t *testing.T) {
	p := nativeProcess(t, context.Background(), "echo")
	writeRequest(t, p)
	if lineWithin(t, p.Stdout) != "private-reply\n" || lineWithin(t, p.Stderr) != "private-stderr\n" {
		t.Fatal("private streams crossed")
	}
	requireInventory(t, p, p.pid)
	for _, f := range []*os.File{p.Stdin.(*writePipe).f, p.Stdout.(*readPipe).f, p.Stderr.(*readPipe).f} {
		requireNoninheritable(t, syscall.Handle(f.Fd()))
	}
	j := p.owner.(*nativeJob)
	func() {
		j.mu.Lock()
		defer j.mu.Unlock()
		requireNoninheritable(t, j.h)
	}()
	if e := p.Stdin.Close(); e != nil {
		t.Fatal("stdin EOF failed")
	}
	r, e := waitTest(t, p)
	if e != nil || !r.Natural() {
		t.Fatal("natural completion failed")
	}
	for _, stream := range []io.Reader{p.Stdout, p.Stderr} {
		b, e := io.ReadAll(stream)
		if e != nil || len(b) != 0 {
			t.Fatal("natural EOF missing")
		}
	}
}
func TestWindowsHandleListExcludesUnknownInheritableHandle(t *testing.T) {
	sa := syscall.SecurityAttributes{Length: uint32(unsafe.Sizeof(syscall.SecurityAttributes{})), InheritHandle: 1}
	event, _, _ := kernel32.NewProc("CreateEventW").Call(uintptr(unsafe.Pointer(&sa)), 1, 0, 0)
	if event == 0 {
		t.Fatal("test event creation failed")
	}
	defer syscall.CloseHandle(syscall.Handle(event))
	s := helperSpec(t, "probe")
	s.Env = append(s.Env, "PREVIEWPROCESS_SENTINEL="+strconv.FormatUint(uint64(event), 10))
	p, e := Start(context.Background(), s)
	if e != nil {
		t.Fatal("probe start failed")
	}
	defer func() { _, _ = p.Stop(0); _ = p.Stdout.Close(); _ = p.Stderr.Close() }()
	writeRequest(t, p)
	if lineWithin(t, p.Stdout) != "private-reply\n" {
		t.Fatal("probe reply missing")
	}
	state, e := syscall.WaitForSingleObject(syscall.Handle(event), 0)
	if e != nil || state != syscall.WAIT_TIMEOUT {
		t.Fatal("unknown inheritable handle leaked")
	}
	requireInventory(t, p, p.pid)
	if _, e = p.Stop(time.Second); e != nil {
		t.Fatal("probe did not exit naturally")
	}
}
func spawnedLeaf(t *testing.T, p *Process) syscall.Handle {
	t.Helper()
	writeRequest(t, p)
	line := lineWithin(t, p.Stdout)
	pid, e := strconv.ParseUint(strings.TrimSpace(line), 10, 32)
	if e != nil {
		t.Fatal("synthetic descendant identity unavailable")
	}
	h := processHandle(t, uint32(pid))
	j := p.owner.(*nativeJob)
	if !j.contains(h) {
		t.Fatal("descendant did not inherit exact job")
	}
	requireInventory(t, p, p.pid, uint32(pid))
	return h
}
func TestWindowsRootCrashRetiresOutputHoldingDescendant(t *testing.T) {
	p := nativeProcess(t, context.Background(), "spawn")
	leaf := spawnedLeaf(t, p)
	outputDone := make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, p.Stdout); close(outputDone) }()
	if _, e := io.WriteString(p.Stdin, "crash\n"); e != nil {
		t.Fatal("crash request failed")
	}
	r, e := waitTest(t, p)
	if !errors.Is(e, ErrForcedCleanup) || r.ExitCode != 7 || r.Cause != CauseDescendants || !r.TreeExited {
		t.Fatal("root crash cleanup failed")
	}
	requireSignal(t, leaf)
	select {
	case <-outputDone:
	case <-time.After(time.Second):
		t.Fatal("orphan stdout held teardown")
	}
}
func TestWindowsZeroExitStillRecordsForcedDescendantCleanup(t *testing.T) {
	p := nativeProcess(t, context.Background(), "spawn")
	leaf := spawnedLeaf(t, p)
	if _, e := io.WriteString(p.Stdin, "zero\n"); e != nil {
		t.Fatal("zero-exit request failed")
	}
	r, e := waitTest(t, p)
	if !errors.Is(e, ErrForcedCleanup) || r.ExitCode != 0 || r.Cause != CauseDescendants || !r.Forced || r.Natural() {
		t.Fatal("zero exit hid forced cleanup")
	}
	requireSignal(t, leaf)
}
func TestWindowsCancellationJoinsOwnedTree(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := nativeProcess(t, ctx, "spawn")
	leaf := spawnedLeaf(t, p)
	cancel()
	r, e := waitTest(t, p)
	if !errors.Is(e, ErrForcedCleanup) || r.Cause != CauseCanceled || !r.RootExited || !r.TreeExited {
		t.Fatal("cancellation failed to join tree")
	}
	requireSignal(t, leaf)
}
func TestWindowsBlockedStdinCannotDeadlockStop(t *testing.T) {
	p := nativeProcess(t, context.Background(), "unread")
	if lineWithin(t, p.Stdout) != "ready\n" {
		t.Fatal("unread helper not ready")
	}
	writerDone := make(chan struct{})
	go func() { _, _ = p.Stdin.Write(make([]byte, 4<<20)); close(writerDone) }()
	select {
	case <-writerDone:
		t.Fatal("synthetic write did not block")
	case <-time.After(50 * time.Millisecond):
	}
	started := time.Now()
	r, e := p.Stop(25 * time.Millisecond)
	if !errors.Is(e, ErrForcedCleanup) || r.Cause != CauseStopDeadline || !r.TreeExited || time.Since(started) > CleanupTimeout+time.Second {
		t.Fatal("blocked stdin stalled bounded Stop")
	}
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("blocked stdin writer did not retire")
	}
}
func TestWindowsBlockedStdinCannotDeadlockCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	p := nativeProcess(t, ctx, "unread")
	if lineWithin(t, p.Stdout) != "ready\n" {
		t.Fatal("unread helper not ready")
	}
	writerDone := make(chan struct{})
	go func() { _, _ = p.Stdin.Write(make([]byte, 4<<20)); close(writerDone) }()
	select {
	case <-writerDone:
		t.Fatal("synthetic write did not block")
	case <-time.After(50 * time.Millisecond):
	}
	cancel()
	r, e := waitTest(t, p)
	if !errors.Is(e, ErrForcedCleanup) || r.Cause != CauseCanceled || !r.TreeExited {
		t.Fatal("blocked stdin stalled cancellation")
	}
	select {
	case <-writerDone:
	case <-time.After(time.Second):
		t.Fatal("canceled writer did not retire")
	}
}
func TestWindowsStartupCanceledBeforeResume(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var retained syscall.Handle
	p, e := startWithHook(ctx, helperSpec(t, "leaf"), func(pid uint32, j *nativeJob) {
		retained = processHandle(t, pid)
		if !j.contains(retained) {
			t.Fatal("suspended child uncontained")
		}
		cancel()
	})
	if p != nil || !errors.Is(e, context.Canceled) || retained == 0 {
		t.Fatal("canceled suspended startup did not fail closed")
	}
	requireSignal(t, retained)
}
func TestWindowsOwnerExitBeforeResumeRetiresSuspendedChild(t *testing.T) {
	// Deliberately do not use Process for the OUTER observer fixture: its root
	// waiter would itself kill the suspended descendant, masking an inner job
	// ownership defect. The observer retains only process handles, never the
	// inner job. Emergency failure cleanup targets only our exact owned handles.
	s := helperSpec(t, "owner-before-resume")
	cmd := exec.Command(s.Path, s.Args...)
	cmd.Env = s.Env
	cmd.Dir = s.Dir
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x8}
	input, e := cmd.StdinPipe()
	if e != nil {
		t.Fatal("owner fixture input failed")
	}
	defer input.Close()
	output, e := cmd.StdoutPipe()
	if e != nil {
		t.Fatal("owner fixture output failed")
	}
	defer output.Close()
	if cmd.Start() != nil {
		t.Fatal("owner fixture start failed")
	}
	var waitOnce sync.Once
	completed := false
	waited := make(chan error, 1)
	waitOwner := func() { waitOnce.Do(func() { go func() { waited <- cmd.Wait() }() }) }
	defer func() {
		if completed {
			return
		}
		_ = cmd.Process.Kill()
		waitOwner()
		select {
		case <-waited:
		case <-time.After(3 * time.Second):
		}
	}()
	if _, e = io.WriteString(input, "private-request\n"); e != nil {
		t.Fatal("owner request failed")
	}
	line := lineWithin(t, output)
	pid, e := strconv.ParseUint(strings.TrimSpace(line), 10, 32)
	if e != nil {
		t.Fatal("suspended child identity unavailable")
	}
	h := processHandle(t, uint32(pid))
	defer func() {
		state, _ := syscall.WaitForSingleObject(h, 0)
		if state == syscall.WAIT_TIMEOUT {
			_ = syscall.TerminateProcess(h, 1)
		}
	}()
	state, e := syscall.WaitForSingleObject(h, 0)
	if e != nil || state != syscall.WAIT_TIMEOUT {
		t.Fatal("suspended child was not alive")
	}
	if _, e = io.WriteString(input, "exit-before-resume\n"); e != nil {
		t.Fatal("owner-exit acknowledgment failed")
	}
	waitOwner()
	select {
	case e := <-waited:
		completed = true
		if e != nil {
			t.Fatal("owner did not exit cleanly")
		}
	case <-time.After(3 * time.Second):
		t.Fatal("owner did not exit")
	}
	requireSignal(t, h)
}
func TestWindowsRepeatedLifecycleReleasesHandles(t *testing.T) {
	count := func() uint32 {
		h, e := syscall.GetCurrentProcess()
		if e != nil {
			t.Fatal("test process unavailable")
		}
		var n uint32
		ok, _, _ := kernel32.NewProc("GetProcessHandleCount").Call(uintptr(h), uintptr(unsafe.Pointer(&n)))
		if ok == 0 {
			t.Fatal("handle count unavailable")
		}
		return n
	}
	run := func() {
		p, e := Start(context.Background(), helperSpec(t, "echo"))
		if e != nil {
			t.Fatal("repeat start failed")
		}
		writeRequest(t, p)
		if lineWithin(t, p.Stdout) != "private-reply\n" {
			t.Fatal("repeat reply missing")
		}
		if _, e = p.Stop(time.Second); e != nil {
			t.Fatal("repeat stop failed")
		}
		_ = p.Stdin.Close()
		_ = p.Stdout.Close()
		_ = p.Stderr.Close()
	}
	run()
	runtime.GC()
	before := count()
	for i := 0; i < 5; i++ {
		run()
	}
	runtime.GC()
	time.Sleep(50 * time.Millisecond)
	if after := count(); after > before+4 {
		t.Fatal("owned native handles accumulated")
	}
}
func TestWindowsBlockedOutputReadsJoinOnStop(t *testing.T) {
	p := nativeProcess(t, context.Background(), "unread")
	if lineWithin(t, p.Stdout) != "ready\n" {
		t.Fatal("unread helper not ready")
	}
	stdoutDone, stderrDone := make(chan struct{}), make(chan struct{})
	go func() { _, _ = io.Copy(io.Discard, p.Stdout); close(stdoutDone) }()
	go func() { _, _ = io.Copy(io.Discard, p.Stderr); close(stderrDone) }()
	for _, done := range []<-chan struct{}{stdoutDone, stderrDone} {
		select {
		case <-done:
			t.Fatal("synthetic output read did not block")
		case <-time.After(30 * time.Millisecond):
		}
	}
	r, e := p.Stop(0)
	if !errors.Is(e, ErrForcedCleanup) || r.Cause != CauseStopDeadline || !r.TreeExited {
		t.Fatal("blocked output reads stalled Stop")
	}
	for _, done := range []<-chan struct{}{stdoutDone, stderrDone} {
		select {
		case <-done:
		case <-time.After(time.Second):
			t.Fatal("owned output read did not retire")
		}
	}
}
