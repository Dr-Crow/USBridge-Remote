//go:build windows

package sourcepreview

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"

	"usbridge_agent/internal/previewprocess"
)

// Every executable in these tests is this Go test binary. No GUI, desktop,
// device, media, input, or private component is invoked.
func platformViewerHelper() bool {
	mode := os.Getenv("TEST_SOURCE_PREVIEW_EVENT")
	if !strings.HasPrefix(mode, "windows-") {
		return false
	}
	if mode == "windows-leaf" {
		for {
			time.Sleep(time.Hour)
		}
	}
	publish := func(ids []int) {
		raw, _ := json.Marshal(ids)
		if os.WriteFile(os.Getenv("TEST_WINDOWS_VIEWER_IDS"), raw, 0600) != nil {
			os.Exit(40)
		}
	}
	if mode == "windows-block-start" {
		publish([]int{os.Getpid()})
		for {
			time.Sleep(time.Hour)
		}
	}
	var request descriptor
	reader := bufio.NewReader(os.Stdin)
	raw, err := reader.ReadBytes('\n')
	if err != nil || json.Unmarshal(raw, &request) != nil {
		os.Exit(41)
	}
	secret := request.KeyB64
	emit := func(kind, reason string) {
		_ = json.NewEncoder(os.Stdout).Encode(viewerEvent{1, kind, request.SessionID, reason})
	}
	emitReady := func() { emit("ready", ""); emit("first_frame", "") }
	emitStop := func(failed bool) {
		if failed {
			emit("stopped", "failed")
		} else {
			emit("stopped", "completed")
		}
	}

	for _, arg := range os.Args {
		if secret != "" && strings.Contains(arg, secret) {
			os.Exit(42)
		}
	}
	ids := []int{os.Getpid()}
	if mode == "windows-crash" || mode == "windows-zero" || mode == "windows-block" {
		exe, err := os.Executable()
		if err != nil {
			os.Exit(43)
		}
		cmd := exec.Command(exe, "--source-preview-stdin")
		cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x8}
		for _, entry := range os.Environ() {
			if !strings.HasPrefix(entry, "TEST_SOURCE_PREVIEW_EVENT=") {
				cmd.Env = append(cmd.Env, entry)
			}
		}
		cmd.Env = append(cmd.Env, "TEST_SOURCE_PREVIEW_EVENT=windows-leaf")
		cmd.Stdout, cmd.Stderr = os.Stdout, os.Stderr
		if cmd.Start() != nil {
			os.Exit(44)
		}
		ids = append(ids, cmd.Process.Pid)
	}
	publish(ids)
	// More than one pipe buffer verifies that production drains stderr rather
	// than waiting for stdout first. Content must never reach an error/log.
	_, _ = io.WriteString(os.Stderr, strings.Repeat("private-stderr-"+secret, 8192))
	emitReady()
	if mode == "windows-block" {
		for {
			time.Sleep(time.Hour)
		}
	}
	_, err = reader.ReadByte()
	switch mode {
	case "windows-crash":
		os.Exit(7)
	case "windows-zero":
		emitStop(false)
		os.Exit(0)
	case "windows-frame":
		emitStop(true)
		os.Exit(0)
	default:
		if err != io.EOF {
			os.Exit(45)
		}
		emitStop(false)
		os.Exit(0)
	}
	return true
}

func windowsViewerSession(t *testing.T, ctx context.Context, mode string) *viewerProcess {
	t.Helper()
	t.Setenv("TEST_SOURCE_PREVIEW_EVENT", mode)
	t.Setenv("TEST_WINDOWS_VIEWER_IDS", filepath.Join(t.TempDir(), "owned-pids.json"))
	v, err := startViewer(ctx, preparedTestViewer(t), descriptor{SchemaVersion: 1, Profile: Profile, SessionID: "test", KeyB64: "private-test-key"})
	if err != nil {
		t.Fatal(err)
	}
	session := v.(*viewerProcess)
	t.Cleanup(func() { _ = session.Stop() })
	return session
}

func windowsViewerHandles(t *testing.T, count int) []syscall.Handle {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	var ids []int
	for time.Now().Before(deadline) {
		raw, err := os.ReadFile(os.Getenv("TEST_WINDOWS_VIEWER_IDS"))
		if err == nil && json.Unmarshal(raw, &ids) == nil && len(ids) == count {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if len(ids) != count {
		t.Fatal("synthetic identities missing")
	}
	handles := make([]syscall.Handle, 0, len(ids))
	for _, pid := range ids {
		h, err := syscall.OpenProcess(syscall.SYNCHRONIZE|0x1000, false, uint32(pid))
		if err != nil {
			t.Fatal("owned process handle unavailable: " + strconv.Itoa(pid))
		}
		t.Cleanup(func() { _ = syscall.CloseHandle(h) })
		handles = append(handles, h)
	}
	return handles
}
func windowsViewerExited(t *testing.T, handles []syscall.Handle) {
	t.Helper()
	for _, handle := range handles {
		state, err := syscall.WaitForSingleObject(handle, 3000)
		if err != nil || state != syscall.WAIT_OBJECT_0 {
			t.Fatal("owned process survived teardown")
		}
	}
}
func windowsViewerWait(t *testing.T, session *viewerProcess) error {
	t.Helper()
	select {
	case <-session.Done():
	case <-time.After(7 * time.Second):
		t.Fatal("protocol readers did not join")
	}
	select {
	case <-session.writeDone:
	default:
		t.Fatal("request writer not joined")
	}
	err := session.Wait()
	if err != nil && (strings.Contains(err.Error(), "private-stderr") || strings.Contains(err.Error(), "private-test-key")) {
		t.Fatal("private child output leaked")
	}
	return err
}

func TestWindowsViewerCooperativeEOFIsNatural(t *testing.T) {
	session := windowsViewerSession(t, context.Background(), "windows-cooperative")
	handles := windowsViewerHandles(t, 1)
	if err := session.Stop(); err != nil {
		t.Fatal(err)
	}
	if err := windowsViewerWait(t, session); err != nil {
		t.Fatal(err)
	}
	if err := session.Stop(); err != nil {
		t.Fatal("repeated Stop lost natural completion")
	}
	windowsViewerExited(t, handles)
}

func TestWindowsViewerCrashRetiresInheritedOutput(t *testing.T) {
	session := windowsViewerSession(t, context.Background(), "windows-crash")
	handles := windowsViewerHandles(t, 2)
	if _, err := session.child.Stdin.Write([]byte("x")); err != nil {
		t.Fatal("crash trigger failed")
	}
	err := windowsViewerWait(t, session)
	if !errors.Is(err, previewprocess.ErrForcedCleanup) {
		t.Fatal("crashed descendant tree not classified as forced cleanup")
	}
	windowsViewerExited(t, handles)
}

func TestWindowsViewerZeroExitStillRecordsForcedCleanup(t *testing.T) {
	session := windowsViewerSession(t, context.Background(), "windows-zero")
	handles := windowsViewerHandles(t, 2)
	if _, err := session.child.Stdin.Write([]byte("x")); err != nil {
		t.Fatal("zero-exit trigger failed")
	}
	err := windowsViewerWait(t, session)
	if !errors.Is(err, previewprocess.ErrForcedCleanup) {
		t.Fatal("exit0 erased forced cleanup")
	}
	windowsViewerExited(t, handles)
}

func TestWindowsViewerBlockedStdinStopAndCancel(t *testing.T) {
	for _, cancelInstead := range []bool{false, true} {
		t.Run(strconv.FormatBool(cancelInstead), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			session := windowsViewerSession(t, ctx, "windows-block")
			handles := windowsViewerHandles(t, 2)
			writer := make(chan struct{})
			go func() { _, _ = session.child.Stdin.Write(bytes.Repeat([]byte("x"), 8<<20)); close(writer) }()
			select {
			case <-writer:
				t.Fatal("synthetic write did not block")
			case <-time.After(100 * time.Millisecond):
			}
			if cancelInstead {
				cancel()
			} else if !errors.Is(session.Stop(), previewprocess.ErrForcedCleanup) {
				t.Fatal("blocked Stop lost forced cleanup")
			}
			if !errors.Is(windowsViewerWait(t, session), previewprocess.ErrForcedCleanup) {
				t.Fatal("blocked lifecycle lost forced cleanup")
			}
			select {
			case <-writer:
			case <-time.After(time.Second):
				t.Fatal("blocked write not released")
			}
			windowsViewerExited(t, handles)
		})
	}
}

func TestWindowsViewerStartupCancellationJoinsBlockedWrite(t *testing.T) {
	t.Setenv("TEST_SOURCE_PREVIEW_EVENT", "windows-block-start")
	t.Setenv("TEST_WINDOWS_VIEWER_IDS", filepath.Join(t.TempDir(), "startup-pid.json"))
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	binary := preparedTestViewer(t)
	// The private launcher is stressed with an oversized synthetic descriptor;
	// Manager's bounded production descriptor and platform gate are unchanged.
	request := descriptor{SchemaVersion: 1, Profile: Profile, SessionID: "test", KeyB64: strings.Repeat("x", 8<<20)}
	result := make(chan error, 1)
	go func() { _, err := startViewer(ctx, binary, request); result <- err }()
	handles := windowsViewerHandles(t, 1)
	select {
	case <-result:
		t.Fatal("startup did not block")
	case <-time.After(100 * time.Millisecond):
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, previewprocess.ErrForcedCleanup) {
			t.Fatal("startup cancellation lost typed cleanup")
		}
	case <-time.After(7 * time.Second):
		t.Fatal("canceled startup stranded its request write")
	}
	windowsViewerExited(t, handles)
}

func TestWindowsManagerRemainsDisabled(t *testing.T) {
	if New().deps.permitted() {
		t.Fatal("production Windows preview gate changed")
	}
}
