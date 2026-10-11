//go:build windows

// SPDX-License-Identifier: GPL-3.0-only
package previewprocess

import (
	"context"
	"errors"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var (
	createJob        = kernel32.NewProc("CreateJobObjectW")
	setJob           = kernel32.NewProc("SetInformationJobObject")
	queryJob         = kernel32.NewProc("QueryInformationJobObject")
	isInJob          = kernel32.NewProc("IsProcessInJob")
	terminateJob     = kernel32.NewProc("TerminateJobObject")
	resumeThread     = kernel32.NewProc("ResumeThread")
	initAttributes   = kernel32.NewProc("InitializeProcThreadAttributeList")
	updateAttributes = kernel32.NewProc("UpdateProcThreadAttribute")
	deleteAttributes = kernel32.NewProc("DeleteProcThreadAttributeList")
)

const (
	jobListAttribute    = 0x0002000d // ProcThreadAttributeJobList | INPUT; Win10/Server2016+
	handleListAttribute = 0x00020002
	launchFlags         = 0x4 | 0x400 | 0x80000 | 0x8 // suspended, Unicode env, extended info, DETACHED_PROCESS
)

type jobLimits struct {
	PerProcessTime, PerJobTime           int64
	Flags                                uint32
	MinimumWorkingSet, MaximumWorkingSet uintptr
	ActiveProcessLimit                   uint32
	Affinity                             uintptr
	PriorityClass, SchedulingClass       uint32
}
type ioCounters struct{ ReadOperations, WriteOperations, OtherOperations, ReadBytes, WriteBytes, OtherBytes uint64 }
type extendedJobLimits struct {
	Basic                                                      jobLimits
	IO                                                         ioCounters
	ProcessMemory, JobMemory, PeakProcessMemory, PeakJobMemory uintptr
}
type jobProcessList struct {
	Assigned, Count uint32
	PIDs            [128]uintptr
}
type startupInfoEx struct {
	Startup    syscall.StartupInfo
	Attributes uintptr
}

type nativeJob struct {
	mu sync.Mutex
	h  syscall.Handle
}

func newJob() (*nativeJob, error) {
	h, _, _ := createJob.Call(0, 0) // unnamed, default security, noninheritable
	if h == 0 {
		return nil, ErrStart
	}
	j := &nativeJob{h: syscall.Handle(h)}
	limits := extendedJobLimits{}
	limits.Basic.Flags = 0x2000 // KILL_ON_JOB_CLOSE only; no breakaway or UI restrictions
	ok, _, _ := setJob.Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		if j.close() != nil {
			return nil, errors.Join(ErrStart, ErrContainment)
		}
		return nil, ErrStart
	}
	return j, nil
}
func (j *nativeJob) inventory() ([]uint32, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.h == 0 {
		return nil, ErrContainment
	}
	var list jobProcessList
	ok, _, _ := queryJob.Call(uintptr(j.h), 3, uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list), 0)
	if ok == 0 || list.Assigned != list.Count || list.Count > uint32(len(list.PIDs)) {
		return nil, ErrContainment
	}
	ids := make([]uint32, list.Count)
	for i := range ids {
		if list.PIDs[i] == 0 || list.PIDs[i] > 0xffffffff {
			return nil, ErrContainment
		}
		ids[i] = uint32(list.PIDs[i])
	}
	return ids, nil
}
func (j *nativeJob) contains(h syscall.Handle) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.h == 0 {
		return false
	}
	var yes int32
	ok, _, _ := isInJob.Call(uintptr(h), uintptr(j.h), uintptr(unsafe.Pointer(&yes)))
	return ok != 0 && yes != 0
}
func (j *nativeJob) terminate() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.h == 0 {
		return ErrContainment
	}
	ok, _, _ := terminateJob.Call(uintptr(j.h), 1)
	if ok == 0 {
		return ErrContainment
	}
	return nil
}
func (j *nativeJob) close() error {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.h == 0 {
		return nil
	}
	h := j.h
	j.h = 0
	if syscall.CloseHandle(h) != nil {
		return ErrContainment
	}
	return nil
}

type readPipe struct{ f *os.File }

func (p *readPipe) Read(b []byte) (int, error) { n, e := p.f.Read(b); return n, sanitizePipeError(e) }
func (p *readPipe) Close() error               { return sanitizePipeError(p.f.Close()) }

type writePipe struct{ f *os.File }

func (p *writePipe) Write(b []byte) (int, error) {
	n, e := p.f.Write(b)
	return n, sanitizePipeError(e)
}
func (p *writePipe) Close() error { return sanitizePipeError(p.f.Close()) }
func sanitizePipeError(e error) error {
	if errors.Is(e, os.ErrClosed) {
		return ErrPipeClosed
	}
	if errors.Is(e, io.ErrClosedPipe) {
		return ErrPipeClosed
	}
	return pipeError(e)
}
func privatePipe() (*os.File, *os.File, error) {
	var r, w syscall.Handle
	// Unlike os.Pipe on Windows, begin with BOTH endpoints noninheritable.
	if syscall.CreatePipe(&r, &w, nil, 0) != nil {
		return nil, nil, ErrStart
	}
	return os.NewFile(uintptr(r), "preview-pipe"), os.NewFile(uintptr(w), "preview-pipe"), nil
}
func start(ctx context.Context, spec Spec) (*Process, error) { return startWithHook(ctx, spec, nil) }

// The per-call seam is used only by this package's synthetic native tests.
// There is no global hook, runtime flag, environment switch, or exported bypass.
func startWithHook(ctx context.Context, spec Spec, beforeResume func(uint32, *nativeJob)) (*Process, error) {
	return startWithHooks(ctx, spec, nativeStartupHooks{beforeResume: beforeResume})
}

type nativeStartupHooks struct {
	beforeResume func(uint32, *nativeJob)
	// Tests may add a fixed failure AFTER the real close. They cannot skip or
	// replace a native close, clear its error, or change launch policy.
	afterClose func(string) error
}

func startWithHooks(ctx context.Context, spec Spec, hooks nativeStartupHooks) (_ *Process, startErr error) {
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	if !validSpec(spec) {
		return nil, ErrInvalidSpec
	}
	// The currently reviewed ABI is 64-bit Windows. Fail closed on other layouts.
	if unsafe.Sizeof(extendedJobLimits{}) != 144 || unsafe.Sizeof(startupInfoEx{}) != 112 || unsafe.Offsetof(jobProcessList{}.PIDs) != 8 {
		return nil, ErrUnsupported
	}
	j, e := newJob()
	if e != nil {
		return nil, e
	}
	success := false
	observeClose := func(role string, err error) error {
		if hooks.afterClose != nil {
			err = errors.Join(err, hooks.afterClose(role))
		}
		return err
	}
	defer func() {
		if !success && j.close() != nil {
			startErr = errors.Join(startErr, ErrContainment)
		}
	}()
	var files []*os.File
	closedFiles := make(map[*os.File]bool)
	closeFile := func(role string, f *os.File) error {
		if closedFiles[f] {
			return nil
		}
		// Never retry a failed native close: the handle's state may be unknown
		// or already recycled. Preserve uncertainty instead of closing an alias.
		closedFiles[f] = true
		return observeClose(role, f.Close())
	}
	defer func() {
		if !success {
			for i, f := range files {
				if closeFile("rollback-pipe-"+string(rune('0'+i)), f) != nil {
					startErr = errors.Join(startErr, ErrContainment)
				}
			}
		}
	}()
	inR, inW, e := privatePipe()
	if e != nil {
		return nil, e
	}
	files = append(files, inR, inW)
	outR, outW, e := privatePipe()
	if e != nil {
		return nil, e
	}
	files = append(files, outR, outW)
	errR, errW, e := privatePipe()
	if e != nil {
		return nil, e
	}
	files = append(files, errR, errW)
	childEnds := []*os.File{inR, outW, errW}
	handles := []syscall.Handle{syscall.Handle(inR.Fd()), syscall.Handle(outW.Fd()), syscall.Handle(errW.Fd())}
	for _, h := range handles {
		if syscall.SetHandleInformation(h, syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT) != nil {
			return nil, ErrStart
		}
	}
	// Startup has exclusive ownership of j until newProcess transfers it to the
	// lifecycle coordinator; no cancellation goroutine can close/reuse j.h here.
	jobs := []syscall.Handle{j.h}
	var size uintptr
	initAttributes.Call(0, 2, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 || size > 65536 {
		return nil, ErrStart
	}
	// uintptr storage supplies pointer alignment for the native attribute list.
	storage := make([]uintptr, (size+unsafe.Sizeof(uintptr(0))-1)/unsafe.Sizeof(uintptr(0)))
	attrs := uintptr(unsafe.Pointer(&storage[0]))
	ok, _, _ := initAttributes.Call(attrs, 2, 0, uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return nil, ErrStart
	}
	defer func() {
		deleteAttributes.Call(attrs)
		runtime.KeepAlive(storage)
		runtime.KeepAlive(handles)
		runtime.KeepAlive(jobs)
	}()
	ok, _, _ = updateAttributes.Call(attrs, 0, handleListAttribute, uintptr(unsafe.Pointer(&handles[0])), uintptr(len(handles))*unsafe.Sizeof(handles[0]), 0, 0)
	if ok == 0 {
		return nil, ErrStart
	}
	ok, _, _ = updateAttributes.Call(attrs, 0, jobListAttribute, uintptr(unsafe.Pointer(&jobs[0])), unsafe.Sizeof(jobs[0]), 0, 0)
	if ok == 0 {
		return nil, ErrStart
	}
	si := startupInfoEx{Attributes: attrs}
	si.Startup.Cb = uint32(unsafe.Sizeof(si))
	si.Startup.Flags = syscall.STARTF_USESTDHANDLES
	si.Startup.StdInput, si.Startup.StdOutput, si.Startup.StdErr = handles[0], handles[1], handles[2]
	// Desktop is intentionally unset. This package never changes desktop/session,
	// token, identity or elevation, and never calls a GUI API.
	line := syscall.EscapeArg(spec.Path)
	for _, arg := range spec.Args {
		line += " " + syscall.EscapeArg(arg)
	}
	cmd, e := syscall.UTF16FromString(line)
	if e != nil || len(cmd) > 32767 {
		return nil, ErrInvalidSpec
	}
	exe, e := syscall.UTF16PtrFromString(spec.Path)
	if e != nil {
		return nil, ErrInvalidSpec
	}
	cwd, e := syscall.UTF16PtrFromString(spec.Dir)
	if e != nil {
		return nil, ErrInvalidSpec
	}
	env, e := environmentBlock(spec.Env)
	if e != nil {
		return nil, e
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	var pi syscall.ProcessInformation
	e = syscall.CreateProcess(exe, &cmd[0], nil, nil, true, launchFlags, &env[0], cwd, &si.Startup, &pi)
	runtime.KeepAlive(storage)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(jobs)
	runtime.KeepAlive(env)
	runtime.KeepAlive(cmd)
	if e != nil {
		return nil, ErrStart
	}
	var waitHandle syscall.Handle
	processClosed, threadClosed := false, false
	closeOriginal := func(role string, h syscall.Handle, attempted *bool) error {
		if *attempted {
			return nil
		}
		*attempted = true
		return observeClose(role, syscall.CloseHandle(h))
	}
	defer func() {
		if success {
			return
		} // All original handles were checked before transfer.
		cleanup := startupCleanupResult{}
		// The child remains atomically owned even after Resume. No lifecycle
		// goroutine starts until all startup-only releases have succeeded.
		cleanup.terminateErr = j.terminate()
		cleanup.jobCloseErr = j.close()
		rootHandle := pi.Process
		if processClosed {
			rootHandle = waitHandle
		}
		if rootHandle != 0 {
			state, waitErr := syscall.WaitForSingleObject(rootHandle, uint32(CleanupTimeout.Milliseconds()))
			cleanup.waitErr = waitErr
			cleanup.rootExited = waitErr == nil && state == syscall.WAIT_OBJECT_0
			cleanup.timedOut = waitErr == nil && state == uint32(syscall.WAIT_TIMEOUT)
		}
		if waitHandle != 0 {
			cleanup.waitHandleCloseErr = observeClose("rollback-wait-handle", syscall.CloseHandle(waitHandle))
		}
		cleanup.processHandleCloseErr = closeOriginal("process-handle", pi.Process, &processClosed)
		cleanup.threadHandleCloseErr = closeOriginal("thread-handle", pi.Thread, &threadClosed)
		startErr = startupCleanupError(startErr, cleanup)
	}()
	if !j.contains(pi.Process) {
		return nil, ErrContainment
	}
	ids, e := j.inventory()
	if e != nil || len(ids) != 1 || ids[0] != pi.ProcessId {
		return nil, ErrContainment
	}
	current, e := syscall.GetCurrentProcess()
	if e != nil {
		return nil, ErrStart
	}
	if syscall.DuplicateHandle(current, pi.Process, current, &waitHandle, 0, false, syscall.DUPLICATE_SAME_ACCESS) != nil {
		return nil, ErrStart
	}
	if hooks.beforeResume != nil {
		hooks.beforeResume(pi.ProcessId, j)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// Release the parent's copies of the inherited child endpoints while the
	// child is still suspended. An uncertain release never reaches Resume.
	childRoles := []string{"child-stdin", "child-stdout", "child-stderr"}
	var pipeCloseErr error
	for i, f := range childEnds {
		pipeCloseErr = errors.Join(pipeCloseErr, closeFile(childRoles[i], f))
	}
	if pipeCloseErr != nil {
		return nil, errors.Join(ErrStart, ErrContainment)
	}
	if ctx.Err() != nil {
		return nil, ctx.Err()
	}
	// Exactly one resume attempt, requiring the expected previous suspend count.
	previous, _, _ := resumeThread.Call(uintptr(pi.Thread))
	if previous != 1 {
		return nil, ErrStart
	}
	// Keep the duplicated wait handle and Job owned locally until both original
	// handles have been checked. Failure after Resume terminates and joins this
	// exact child before returning an error; no usable Process is published.
	if closeOriginal("thread-handle", pi.Thread, &threadClosed) != nil {
		return nil, errors.Join(ErrStart, ErrContainment)
	}
	if closeOriginal("process-handle", pi.Process, &processClosed) != nil {
		return nil, errors.Join(ErrStart, ErrContainment)
	}
	root := make(chan rootExit, 1)
	waiterDone := make(chan error, 1)
	abandon := make(chan struct{})
	go waitRoot(waitHandle, root, waiterDone, abandon)
	p := newProcess(ctx, j, pi.ProcessId, &writePipe{inW}, &readPipe{outR}, &readPipe{errR}, root, waiterDone, abandon)
	success = true
	return p, nil
}
func environmentBlock(env []string) ([]uint16, error) {
	entries := append([]string(nil), env...)
	sort.Slice(entries, func(i, k int) bool { return strings.ToUpper(entries[i]) < strings.ToUpper(entries[k]) })
	var block []uint16
	for _, entry := range entries {
		u, e := syscall.UTF16FromString(entry)
		if e != nil {
			return nil, ErrInvalidSpec
		}
		block = append(block, u...)
	}
	block = append(block, 0)
	if len(block) == 1 {
		block = append(block, 0)
	} // Explicit empty Unicode environment.
	if len(block) > 32767 {
		return nil, ErrInvalidSpec
	}
	return block, nil
}
func waitRoot(h syscall.Handle, out chan<- rootExit, done chan<- error, release <-chan struct{}) {
	defer func() {
		// The coordinator has retired the tree or reached its cleanup deadline.
		// Only this waiter closes h, and never during a native wait.
		var err error
		if syscall.CloseHandle(h) != nil {
			err = ErrContainment
		}
		done <- err
		close(done)
	}()
	report := func(exit rootExit) {
		out <- exit
		// Retain the root kernel object until inventory/cleanup finishes. Otherwise
		// PID reuse could confuse the coordinator's retiring-root accounting check.
		<-release
	}
	for {
		state, e := syscall.WaitForSingleObject(h, 25)
		if e != nil {
			report(rootExit{err: ErrContainment})
			return
		}
		if state == syscall.WAIT_OBJECT_0 {
			exit := rootExit{}
			if syscall.GetExitCodeProcess(h, &exit.code) != nil {
				exit.err = ErrContainment
			}
			report(exit)
			return
		}
		if state != uint32(syscall.WAIT_TIMEOUT) {
			report(rootExit{err: ErrContainment})
			return
		}
		select {
		case <-release:
			return
		default:
		}
	}
}
