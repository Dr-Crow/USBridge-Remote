// SPDX-License-Identifier: GPL-3.0-only
// Package previewprocess owns a Windows pipe-only child and its ordinary
// descendants. It is lifecycle containment, not a hostile-code sandbox.
package previewprocess

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"time"
)

// Spec is an explicit launch. Args excludes argv[0]. Env is the complete child
// environment; nil means empty, never inheritance. Path and Dir must be absolute
// local-drive Windows paths. Path must name an .exe. Validation is lexical, not
// executable pinning or a reparse-point/security check.
type Spec struct {
	Path      string
	Args, Env []string
	Dir       string
}
type codeError string

func (e codeError) Error() string { return string(e) }

var (
	ErrUnsupported    = codeError("previewprocess: unsupported platform")
	ErrInvalidSpec    = codeError("previewprocess: invalid launch specification")
	ErrStart          = codeError("previewprocess: native startup failed")
	ErrContainment    = codeError("previewprocess: containment verification failed")
	ErrForcedCleanup  = codeError("previewprocess: forced tree cleanup")
	ErrCleanupTimeout = codeError("previewprocess: cleanup deadline exceeded")
	ErrExitStatus     = codeError("previewprocess: child exited unsuccessfully")
	ErrPipe           = codeError("previewprocess: pipe operation failed")
	ErrPipeClosed     = codeError("previewprocess: pipe closed")
)

type Cause string

const (
	CauseNatural        Cause = "natural"
	CauseCanceled       Cause = "context_canceled"
	CauseStopDeadline   Cause = "stop_deadline"
	CauseDescendants    Cause = "residual_descendants"
	CauseContainment    Cause = "containment_failure"
	CauseCleanupTimeout Cause = "cleanup_timeout"
	MaxStopGrace              = 30 * time.Second
	CleanupTimeout            = 3 * time.Second
)

// Result separates observed exit status from why cleanup occurred. In
// particular, ExitCode == 0 does not imply natural completion. TreeExited is
// true only after the owned job was observed empty.
type Result struct {
	ExitCode                       uint32
	RootExited, TreeExited, Forced bool
	Cause                          Cause
}

func (r Result) Natural() bool {
	return r.RootExited && r.TreeExited && !r.Forced && r.Cause == CauseNatural
}
func resultError(r Result) error {
	if !r.RootExited || !r.TreeExited || r.Cause == CauseCleanupTimeout {
		return ErrCleanupTimeout
	}
	if r.Cause == CauseContainment {
		return ErrContainment
	}
	if r.Forced {
		return ErrForcedCleanup
	}
	if r.ExitCode != 0 {
		return ErrExitStatus
	}
	return nil
}

// startupCleanupResult records failed-start cleanup separately from its original
// cause. Native errors are observations only: never include their text in the
// caller-visible error. A successful wait must explicitly observe root exit.
type startupCleanupResult struct {
	terminateErr, jobCloseErr, waitErr, waitHandleCloseErr error
	processHandleCloseErr, threadHandleCloseErr            error
	rootExited, timedOut                                   bool
}

func startupCleanupError(cause error, r startupCleanupResult) error {
	var uncertainty error
	if r.terminateErr != nil || r.jobCloseErr != nil || r.waitErr != nil || r.waitHandleCloseErr != nil || r.processHandleCloseErr != nil || r.threadHandleCloseErr != nil || (!r.rootExited && !r.timedOut) {
		uncertainty = ErrContainment
	}
	if r.timedOut {
		uncertainty = errors.Join(uncertainty, ErrCleanupTimeout)
	}
	if uncertainty == nil {
		return cause
	}
	return errors.Join(cause, uncertainty)
}

// Process exposes private streams, without exposing native handles. Streams are
// safe for concurrent use/Close. Callers own reading and closing Stdout/Stderr;
// successful natural completion preserves unread output for draining to EOF.
// Forced cleanup closes output streams. Done/Wait do not depend on readers.
// No method may be called on a zero Process.
type Process struct {
	Stdin                               io.WriteCloser
	Stdout, Stderr                      io.ReadCloser
	done                                chan struct{}
	wake                                chan struct{}
	stopMu                              sync.Mutex
	stopAt                              time.Time
	result                              Result // immutable once done closes
	owner                               treeOwner
	root                                <-chan rootExit
	waiterDone                          <-chan error
	abandonWait                         chan struct{}
	stdinClose                          sync.Once
	outputClose                         sync.Once
	input                               io.WriteCloser
	output, errors                      io.ReadCloser
	inputClosed                         chan struct{}
	outputsClosed                       chan struct{}
	inputCloseFailed, outputCloseFailed bool
	pid                                 uint32
}
type rootExit struct {
	code uint32
	err  error
}

// Only the lifecycle goroutine uses owner after startup completes.
type treeOwner interface {
	inventory() ([]uint32, error)
	terminate() error
	close() error
}

// Start creates a suspended child already in its own kill-on-close job, verifies
// exact initial ownership, and resumes once. Unsupported APIs fail closed.
func Start(ctx context.Context, spec Spec) (*Process, error) {
	if ctx == nil {
		return nil, ErrInvalidSpec
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if !validSpec(spec) {
		return nil, ErrInvalidSpec
	}
	// The caller must not mutate slices concurrently with Start. Copy before
	// native setup so retained state never aliases the caller's slices.
	spec.Args = append([]string(nil), spec.Args...)
	spec.Env = append([]string(nil), spec.Env...)
	return start(ctx, spec)
}
func (p *Process) Done() <-chan struct{} { return p.done }
func (p *Process) Wait() (Result, error) { <-p.done; return p.result, resultError(p.result) }

// Stop closes stdin and allows grace for cooperative exit. At the earliest
// concurrent Stop deadline it forces this tree. Negative grace means zero;
// grace above MaxStopGrace is clamped. Cancellation skips grace. A caller waits
// at most the clamped grace plus CleanupTimeout (apart from Go scheduling).
// On timeout the returned snapshot is conservative; Wait remains authoritative.
func (p *Process) Stop(grace time.Duration) (Result, error) {
	select {
	case <-p.done:
		return p.Wait()
	default:
	}
	if grace < 0 {
		grace = 0
	}
	if grace > MaxStopGrace {
		grace = MaxStopGrace
	}
	deadline := time.Now().Add(grace)
	p.stopMu.Lock()
	if p.stopAt.IsZero() || deadline.Before(p.stopAt) {
		p.stopAt = deadline
	}
	p.stopMu.Unlock()
	select {
	case p.wake <- struct{}{}:
	default:
	}
	timer := time.NewTimer(grace + CleanupTimeout)
	defer timer.Stop()
	select {
	case <-p.done:
		return p.Wait()
	case <-timer.C:
		// Prefer a completion concurrent with the timer.
		select {
		case <-p.done:
			return p.Wait()
		default:
		}
		return Result{Forced: true, Cause: CauseCleanupTimeout}, ErrCleanupTimeout
	}
}
func (p *Process) closeInput() {
	p.stdinClose.Do(func() { go func() { defer close(p.inputClosed); p.inputCloseFailed = closeFailed(p.input.Close()) }() })
}
func closeFailed(err error) bool { return err != nil && !errors.Is(err, ErrPipeClosed) }
func (p *Process) closeOutputs() {
	p.outputClose.Do(func() {
		// Never put a pipe Close on the lifecycle/force-deadline path. Keep private
		// references so replacing an exported stream cannot replace owned cleanup.
		go func() {
			failures := make(chan bool, 2)
			go func() { failures <- closeFailed(p.output.Close()) }()
			go func() { failures <- closeFailed(p.errors.Close()) }()
			for range 2 {
				if <-failures {
					p.outputCloseFailed = true
				}
			}
			close(p.outputsClosed)
		}()
	})
}
func (p *Process) stopDeadline() time.Time { p.stopMu.Lock(); defer p.stopMu.Unlock(); return p.stopAt }
func newProcess(ctx context.Context, owner treeOwner, pid uint32, in io.WriteCloser, out, stderr io.ReadCloser, root <-chan rootExit, waiterDone <-chan error, abandon chan struct{}) *Process {
	p := &Process{Stdin: in, Stdout: out, Stderr: stderr, input: in, output: out, errors: stderr, inputClosed: make(chan struct{}), outputsClosed: make(chan struct{}), done: make(chan struct{}), wake: make(chan struct{}, 1), owner: owner, pid: pid, root: root, waiterDone: waiterDone, abandonWait: abandon}
	go p.lifecycle(ctx)
	return p
}
func (p *Process) lifecycle(ctx context.Context) {
	r := Result{Cause: CauseNatural}
	var cleanupAt time.Time
	force := func(c Cause) {
		if r.Forced {
			return
		}
		r.Forced, r.Cause = true, c
		cleanupAt = time.Now().Add(CleanupTimeout)
		p.closeInput()
		if p.owner.terminate() != nil {
			r.Cause = CauseContainment
			_ = p.owner.close()
		}
	}
	ticker := time.NewTicker(10 * time.Millisecond)
	defer ticker.Stop()
	ctxDone := ctx.Done()
	rootCh := p.root
	for {
		select {
		case exit := <-rootCh:
			rootCh = nil
			r.RootExited, r.ExitCode = true, exit.code
			if cleanupAt.IsZero() {
				cleanupAt = time.Now().Add(CleanupTimeout)
			}
			p.closeInput()
			if exit.err != nil {
				r.RootExited = false
				force(CauseContainment)
			}
		case <-ctxDone:
			ctxDone = nil
			force(CauseCanceled)
		case <-p.wake:
			p.closeInput()
		case <-ticker.C:
		}
		if !r.Forced {
			at := p.stopDeadline()
			if !at.IsZero() && !time.Now().Before(at) && !r.RootExited {
				force(CauseStopDeadline)
			}
		}
		if r.RootExited || r.Forced {
			ids, err := p.owner.inventory()
			if err != nil {
				force(CauseContainment)
			} else {
				if len(ids) == 0 && r.RootExited {
					r.TreeExited = true
					break
				}
				if r.RootExited {
					// A signaled root can briefly remain in the accounting list. Do not
					// mislabel that as forced descendant cleanup.
					for _, id := range ids {
						if id != p.pid {
							force(CauseDescendants)
							break
						}
					}
				}
			}
		}
		if !cleanupAt.IsZero() && !time.Now().Before(cleanupAt) {
			if !r.Forced {
				force(CauseCleanupTimeout)
			}
			break
		}
	}
	// Last close is always the OS crash-safety net, including API failures. The
	// waiter alone owns its process handle and closes it only between waits.
	if p.owner.close() != nil {
		r.Forced = true
		r.Cause = CauseContainment
	}
	close(p.abandonWait)
	if r.Forced {
		p.closeOutputs()
	}
	p.closeInput()
	// Join package-owned cleanup work within the same cleanup budget. Output
	// readers belong to the caller; closing their files interrupts native I/O,
	// but we do not claim to join arbitrary caller code.
	join := func(done <-chan struct{}) bool {
		timer := time.NewTimer(max(0, time.Until(cleanupAt)))
		defer timer.Stop()
		select {
		case <-done:
			return true
		default:
		}
		select {
		case <-done:
			return true
		case <-timer.C:
			return false
		}
	}
	waiterTimer := time.NewTimer(max(0, time.Until(cleanupAt)))
	select {
	case err := <-p.waiterDone:
		if err != nil {
			r.Forced = true
			r.Cause = CauseContainment
		}
	case <-waiterTimer.C:
		r.Forced = true
		r.Cause = CauseCleanupTimeout
	}
	waiterTimer.Stop()
	if !join(p.inputClosed) {
		r.Forced = true
		r.Cause = CauseCleanupTimeout
	} else if p.inputCloseFailed {
		r.Forced = true
		if r.Cause != CauseCleanupTimeout {
			r.Cause = CauseContainment
		}
	}
	if r.Forced {
		p.closeOutputs()
		if !join(p.outputsClosed) {
			r.Cause = CauseCleanupTimeout
		} else if p.outputCloseFailed && r.Cause != CauseCleanupTimeout {
			r.Cause = CauseContainment
		}
	}
	p.result = r
	close(p.done)
}

func validSpec(s Spec) bool {
	if !localPath(s.Path) || !strings.HasSuffix(strings.ToLower(s.Path), ".exe") || !localPath(s.Dir) {
		return false
	}
	if len(s.Path)+len(s.Dir) > 32760 || len(s.Args) > 4096 || len(s.Env) > 4096 {
		return false
	}
	total := len(s.Path)
	for _, a := range s.Args {
		if strings.ContainsRune(a, 0) {
			return false
		}
		total += len(a) + 3
		if total > 32760 {
			return false
		}
	}
	keys := make(map[string]bool, len(s.Env))
	total = 0
	for _, e := range s.Env {
		key, _, ok := strings.Cut(e, "=")
		if !ok || key == "" || strings.ContainsAny(e, "\x00") {
			return false
		}
		key = strings.ToUpper(key)
		if keys[key] {
			return false
		}
		keys[key] = true
		total += len(e) + 1
		if total > 32760 {
			return false
		}
	}
	return true
}
func localPath(s string) bool {
	if len(s) < 3 || !((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) || s[1] != ':' || s[2] != '\\' {
		return false
	}
	if strings.ContainsAny(s[3:], ":/\x00<>\"|?*") {
		return false
	}
	if len(s) == 3 {
		return true
	}
	for _, part := range strings.Split(s[3:], `\`) {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		base, _, _ := strings.Cut(strings.ToUpper(part), ".")
		if base == "CON" || base == "PRN" || base == "AUX" || base == "NUL" || (len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9') {
			return false
		}
	}
	return true
}

// Keep errors intentionally content-free. No paths, argv, environment, native
// OS message strings, or child output are attached to an error.
func pipeError(err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, io.EOF) {
		return io.EOF
	}
	return ErrPipe
}
