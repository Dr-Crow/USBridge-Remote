package previewprocess

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"
	"testing"
	"time"
)

func validTestSpec() Spec {
	return Spec{Path: `C:\owned\child.exe`, Dir: `C:\owned`, Args: []string{"--pipe"}, Env: []string{"SystemRoot=C:\\Windows"}}
}
func TestSpecContract(t *testing.T) {
	if !validSpec(validTestSpec()) {
		t.Fatal("valid explicit specification rejected")
	}
	paths := []string{"child.exe", `\\server\share\child.exe`, `\\?\C:\child.exe`, `C:child.exe`, `C:\child.exe:stream`, `C:\..\child.exe`, `C:\CON.exe`, `C:\child.exe `, `C:\child.cmd`, `C:/child.exe`, "C:\\child\x00.exe"}
	for _, path := range paths {
		s := validTestSpec()
		s.Path = path
		if validSpec(s) {
			t.Fatal("unsafe path accepted")
		}
	}
	for _, env := range [][]string{{"no-value"}, {"=secret"}, {"A=one", "a=two"}, {"A=secret\x00other"}} {
		s := validTestSpec()
		s.Env = env
		if validSpec(s) {
			t.Fatal("invalid environment accepted")
		}
	}
	s := validTestSpec()
	s.Env = nil
	if !validSpec(s) {
		t.Fatal("explicit empty environment rejected")
	}
	s.Args = []string{"hidden\x00value"}
	if validSpec(s) {
		t.Fatal("NUL argument accepted")
	}
	if p, e := Start(nil, validTestSpec()); p != nil || !errors.Is(e, ErrInvalidSpec) {
		t.Fatal("nil context accepted")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if p, e := Start(ctx, validTestSpec()); p != nil || !errors.Is(e, context.Canceled) {
		t.Fatal("pre-canceled startup not rejected")
	}
}

type fakeOwner struct {
	mu             sync.Mutex
	ids            []uint32
	forced, closed bool
	onForce        func()
}

func (f *fakeOwner) inventory() ([]uint32, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]uint32(nil), f.ids...), nil
}
func (f *fakeOwner) terminate() error {
	f.mu.Lock()
	f.forced = true
	f.ids = nil
	fn := f.onForce
	f.mu.Unlock()
	if fn != nil {
		fn()
	}
	return nil
}
func (f *fakeOwner) close() error      { f.mu.Lock(); defer f.mu.Unlock(); f.closed = true; return nil }
func (f *fakeOwner) set(ids ...uint32) { f.mu.Lock(); f.ids = ids; f.mu.Unlock() }

type testWriter struct{ closeFn func() error }

func (*testWriter) Write(b []byte) (int, error) { return len(b), nil }
func (w *testWriter) Close() error {
	if w.closeFn != nil {
		return w.closeFn()
	}
	return nil
}

type testReader struct {
	*bytes.Reader
	mu     sync.Mutex
	closed bool
}

func (r *testReader) Close() error { r.mu.Lock(); defer r.mu.Unlock(); r.closed = true; return nil }
func testProcess(ctx context.Context, f *fakeOwner, in io.WriteCloser) (*Process, chan rootExit, *testReader) {
	root := make(chan rootExit, 1)
	waited := make(chan error)
	close(waited)
	out := &testReader{Reader: bytes.NewReader([]byte("buffered output"))}
	p := newProcess(ctx, f, 1, in, out, io.NopCloser(bytes.NewReader(nil)), root, waited, make(chan struct{}))
	return p, root, out
}
func waitTest(t *testing.T, p *Process) (Result, error) {
	t.Helper()
	select {
	case <-p.Done():
		return p.Wait()
	case <-time.After(CleanupTimeout + time.Second):
		t.Fatal("lifecycle blocked")
		return Result{}, ErrCleanupTimeout
	}
}
func TestNaturalWaitPreservesUnreadOutput(t *testing.T) {
	f := &fakeOwner{}
	p, root, out := testProcess(context.Background(), f, &testWriter{})
	root <- rootExit{code: 0}
	r, e := waitTest(t, p)
	if e != nil || !r.Natural() {
		t.Fatal("natural completion misclassified")
	}
	out.mu.Lock()
	closed := out.closed
	out.mu.Unlock()
	if closed {
		t.Fatal("natural Wait discarded unread output")
	}
	b, e := io.ReadAll(p.Stdout)
	if e != nil || string(b) != "buffered output" {
		t.Fatal("unread output was not preserved")
	}
	r2, e := p.Wait()
	if e != nil || r2 != r {
		t.Fatal("Wait is not repeatable")
	}
}
func TestZeroRootExitWithDescendantIsForced(t *testing.T) {
	f := &fakeOwner{ids: []uint32{2}}
	p, root, _ := testProcess(context.Background(), f, &testWriter{})
	root <- rootExit{code: 0}
	r, e := waitTest(t, p)
	if !errors.Is(e, ErrForcedCleanup) || r.ExitCode != 0 || !r.Forced || r.Cause != CauseDescendants || r.Natural() || !r.TreeExited {
		t.Fatal("zero status hid forced descendant cleanup")
	}
}
func TestRootAccountingLagIsNatural(t *testing.T) {
	f := &fakeOwner{ids: []uint32{1}}
	p, root, _ := testProcess(context.Background(), f, &testWriter{})
	root <- rootExit{code: 0}
	time.AfterFunc(30*time.Millisecond, func() { f.set() })
	r, e := waitTest(t, p)
	if e != nil || !r.Natural() {
		t.Fatal("retiring root was mistaken for descendant")
	}
}
func TestContextCancellationCannotBlockOnStdinClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	gate := make(chan struct{})
	var release sync.Once
	f := &fakeOwner{ids: []uint32{1}}
	p, root, _ := testProcess(ctx, f, &testWriter{closeFn: func() error { <-gate; return nil }})
	f.mu.Lock()
	f.onForce = func() { release.Do(func() { close(gate) }); root <- rootExit{code: 0} }
	f.mu.Unlock()
	cancel()
	r, e := waitTest(t, p)
	if !errors.Is(e, ErrForcedCleanup) || r.Cause != CauseCanceled || r.ExitCode != 0 || r.Natural() {
		t.Fatal("cancellation failed to preserve forced cause")
	}
}
func TestConcurrentStopUsesEarliestDeadline(t *testing.T) {
	gate := make(chan struct{})
	var release sync.Once
	f := &fakeOwner{ids: []uint32{1}}
	p, root, _ := testProcess(context.Background(), f, &testWriter{closeFn: func() error { <-gate; return nil }})
	f.mu.Lock()
	f.onForce = func() { release.Do(func() { close(gate) }); root <- rootExit{code: 0} }
	f.mu.Unlock()
	long := make(chan struct{})
	go func() { _, _ = p.Stop(time.Hour); close(long) }()
	r, e := p.Stop(0)
	if !errors.Is(e, ErrForcedCleanup) || r.Cause != CauseStopDeadline || !r.TreeExited {
		t.Fatal("Stop did not force its own tree")
	}
	select {
	case <-long:
	case <-time.After(time.Second):
		t.Fatal("concurrent Stop remained blocked")
	}
	r2, e := p.Stop(-time.Second)
	if !errors.Is(e, ErrForcedCleanup) || r2 != r {
		t.Fatal("repeated Stop changed result")
	}
}
func TestGracefulStopClosesInput(t *testing.T) {
	f := &fakeOwner{ids: []uint32{1}}
	closed := make(chan struct{})
	var once sync.Once
	p, root, _ := testProcess(context.Background(), f, &testWriter{closeFn: func() error { once.Do(func() { close(closed) }); return nil }})
	go func() { <-closed; f.set(); root <- rootExit{code: 0} }()
	r, e := p.Stop(time.Second)
	if e != nil || !r.Natural() {
		t.Fatal("graceful stdin-EOF exit misclassified")
	}
}
func TestNonzeroNaturalExitIsNotSuccess(t *testing.T) {
	f := &fakeOwner{}
	p, root, _ := testProcess(context.Background(), f, &testWriter{})
	root <- rootExit{code: 7}
	r, e := waitTest(t, p)
	if !errors.Is(e, ErrExitStatus) || !r.Natural() || r.ExitCode != 7 {
		t.Fatal("exit status contract changed")
	}
}
func TestErrorsDoNotIncludeNativeContent(t *testing.T) {
	secret := errors.New(`C:\secret\child.exe --secret env=value child-output`)
	if e := pipeError(secret); e != ErrPipe {
		t.Fatal("native error was retained")
	}
	if pipeError(io.EOF) != io.EOF || pipeError(fmt.Errorf("sensitive detail: %w", io.EOF)) != io.EOF || pipeError(nil) != nil {
		t.Fatal("EOF contract changed")
	}
	r := Result{RootExited: true, TreeExited: true, ExitCode: 0, Forced: true, Cause: CauseStopDeadline}
	if resultError(r) != ErrForcedCleanup || r.Natural() {
		t.Fatal("forced zero-exit status became clean")
	}
}

func TestDoneWaitsForOwnedCloserAfterTreeExit(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	var once sync.Once
	f := &fakeOwner{}
	p, root, _ := testProcess(context.Background(), f, &testWriter{closeFn: func() error {
		once.Do(func() { close(started) })
		<-release
		return nil
	}})
	root <- rootExit{code: 0}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("owned close was not started")
	}
	select {
	case <-p.Done():
		t.Fatal("Done preceded owned close completion")
	case <-time.After(30 * time.Millisecond):
	}
	close(release)
	r, e := waitTest(t, p)
	if e != nil || !r.Natural() {
		t.Fatal("joined natural cleanup misclassified")
	}
}
func TestOwnedCloserTimeoutCannotClaimJoinedCleanup(t *testing.T) {
	t.Parallel()
	started, release := make(chan struct{}), make(chan struct{})
	defer close(release)
	f := &fakeOwner{}
	p, root, _ := testProcess(context.Background(), f, &testWriter{closeFn: func() error { close(started); <-release; return nil }})
	root <- rootExit{code: 0}
	select {
	case <-started:
	case <-time.After(time.Second):
		t.Fatal("owned close was not started")
	}
	start := time.Now()
	_, e := p.Stop(0)
	if !errors.Is(e, ErrCleanupTimeout) || time.Since(start) > CleanupTimeout+500*time.Millisecond {
		t.Fatal("Stop did not bound a stalled closer")
	}
	select {
	case <-p.Done():
	case <-time.After(200 * time.Millisecond):
		t.Fatal("closer timeout did not publish completion")
	}
	r, e := p.Wait()
	if !errors.Is(e, ErrCleanupTimeout) || !r.RootExited || !r.TreeExited || !r.Forced || r.Cause != CauseCleanupTimeout || r.Natural() {
		t.Fatal("unfinished closer was called fully joined")
	}
}

type stalledOwner struct {
	fakeOwner
	entered, release chan struct{}
}

func (f *stalledOwner) terminate() error {
	close(f.entered)
	<-f.release
	return f.fakeOwner.terminate()
}
func TestStopCallerDeadlineSurvivesStalledNativeOperation(t *testing.T) {
	t.Parallel()
	f := &stalledOwner{fakeOwner: fakeOwner{ids: []uint32{1}}, entered: make(chan struct{}), release: make(chan struct{})}
	root := make(chan rootExit, 1)
	waited := make(chan error)
	close(waited)
	f.onForce = func() { root <- rootExit{code: 0} }
	p := newProcess(context.Background(), f, 1, &testWriter{}, io.NopCloser(bytes.NewReader(nil)), io.NopCloser(bytes.NewReader(nil)), root, waited, make(chan struct{}))
	done := make(chan error, 1)
	go func() { _, e := p.Stop(0); done <- e }()
	select {
	case <-f.entered:
	case <-time.After(time.Second):
		t.Fatal("force operation was not entered")
	}
	select {
	case e := <-done:
		if !errors.Is(e, ErrCleanupTimeout) {
			t.Fatal("stalled operation lacked timeout")
		}
	case <-time.After(CleanupTimeout + 500*time.Millisecond):
		t.Fatal("caller was blocked by native operation")
	}
	close(f.release)
	select {
	case <-p.Done():
	case <-time.After(time.Second):
		t.Fatal("released lifecycle did not finish")
	}
	if r, e := p.Wait(); !r.Forced || e == nil {
		t.Fatal("late cleanup hid forced cause")
	}
}

func TestOwnedCloserFailureCannotClaimNaturalCleanup(t *testing.T) {
	f := &fakeOwner{}
	p, root, _ := testProcess(context.Background(), f, &testWriter{closeFn: func() error { return ErrPipe }})
	root <- rootExit{code: 0}
	r, e := waitTest(t, p)
	if !errors.Is(e, ErrContainment) || !r.RootExited || !r.TreeExited || !r.Forced || r.Natural() {
		t.Fatal("failed owned close was called clean")
	}
}
func TestAlreadyClosedInputIsNotCleanupFailure(t *testing.T) {
	f := &fakeOwner{}
	p, root, _ := testProcess(context.Background(), f, &testWriter{closeFn: func() error { return ErrPipeClosed }})
	root <- rootExit{code: 0}
	r, e := waitTest(t, p)
	if e != nil || !r.Natural() {
		t.Fatal("already closed input was called a cleanup failure")
	}
}

func TestWaiterReleaseFollowsTreeRetirement(t *testing.T) {
	f := &fakeOwner{}
	root := make(chan rootExit, 1)
	waiterDone := make(chan error, 1)
	release := make(chan struct{})
	p := newProcess(context.Background(), f, 1, &testWriter{}, io.NopCloser(bytes.NewReader(nil)), io.NopCloser(bytes.NewReader(nil)), root, waiterDone, release)
	go func() {
		root <- rootExit{code: 0}
		<-release
		f.mu.Lock()
		closed := f.closed
		f.mu.Unlock()
		if closed {
			waiterDone <- nil
		} else {
			waiterDone <- ErrContainment
		}
		close(waiterDone)
	}()
	r, e := waitTest(t, p)
	if e != nil || !r.Natural() {
		t.Fatal("retained waiter was not joined after tree retirement")
	}
}
func TestWaiterHandleCloseFailureCannotClaimNatural(t *testing.T) {
	f := &fakeOwner{}
	root := make(chan rootExit, 1)
	waiterDone := make(chan error, 1)
	waiterDone <- ErrContainment
	close(waiterDone)
	p := newProcess(context.Background(), f, 1, &testWriter{}, io.NopCloser(bytes.NewReader(nil)), io.NopCloser(bytes.NewReader(nil)), root, waiterDone, make(chan struct{}))
	root <- rootExit{code: 0}
	r, e := waitTest(t, p)
	if !errors.Is(e, ErrContainment) || !r.Forced || r.Natural() {
		t.Fatal("failed retained-handle close became natural success")
	}
}
