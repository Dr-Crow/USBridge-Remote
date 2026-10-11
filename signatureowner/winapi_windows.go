//go:build windows && amd64

// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import (
	"bufio"
	"io"
	"os"
	"runtime"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"syscall"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var createJob = kernel32.NewProc("CreateJobObjectW")
var setJob = kernel32.NewProc("SetInformationJobObject")
var queryJob = kernel32.NewProc("QueryInformationJobObject")
var isInJob = kernel32.NewProc("IsProcessInJob")
var resumeThread = kernel32.NewProc("ResumeThread")
var initAttributes = kernel32.NewProc("InitializeProcThreadAttributeList")
var updateAttributes = kernel32.NewProc("UpdateProcThreadAttribute")
var deleteAttributes = kernel32.NewProc("DeleteProcThreadAttributeList")
var imageName = kernel32.NewProc("QueryFullProcessImageNameW")

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
type job struct {
	resources           *resourceTracker
	handle              syscall.Handle
	once                sync.Once
	closed              atomic.Bool
	mu                  sync.Mutex
	launchCleanupFailed bool
	// Internal suspended identity verification; never supplied by a caller.
	beforeResume func(uint32)
}

func newJob(resources *resourceTracker) (*job, error) {
	h, _, _ := createJob.Call(0, 0)
	if h == 0 {
		return nil, failure("job_create_failed")
	}
	j := &job{handle: syscall.Handle(h), resources: resources}
	limits := extendedJobLimits{}
	limits.Basic.Flags = 0x2000 // JOB_OBJECT_LIMIT_KILL_ON_JOB_CLOSE; deliberately no breakaway flags.
	ok, _, _ := setJob.Call(h, 9, uintptr(unsafe.Pointer(&limits)), unsafe.Sizeof(limits))
	if ok == 0 {
		j.close()
		return nil, failure("job_limits_failed")
	}
	return j, nil
}
func (j *job) close() {
	j.once.Do(func() {
		j.mu.Lock()
		defer j.mu.Unlock()
		j.closed.Store(true)
		j.resources.record("job_handle", syscall.CloseHandle(j.handle))
	})
}

type jobProcessList struct {
	Assigned, Count uint32
	PIDs            [128]uintptr
}

func (j *job) pids() ([]uint32, error) {
	pids, _, err := j.pidsSnapshot()
	return pids, err
}

// Keep the exact queried list for bounded native-test diagnostics.
func (j *job) pidsSnapshot() ([]uint32, jobProcessList, error) {
	j.mu.Lock()
	defer j.mu.Unlock()
	var list jobProcessList
	if j.closed.Load() {
		return nil, list, failure("safety_job_closed")
	}
	ok, _, _ := queryJob.Call(uintptr(j.handle), 3, uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list), 0)
	return decodeInventory(list, ok != 0)
}

func decodeInventory(list jobProcessList, queried bool) ([]uint32, jobProcessList, error) {
	if !queried || list.Count > 128 || list.Assigned > 128 || list.Count > list.Assigned {
		return nil, list, failure("job_inventory_failed")
	}
	pids := make([]uint32, list.Count)
	for i := range pids {
		if list.PIDs[i] == 0 || list.PIDs[i] > 0xffffffff {
			return nil, list, failure("job_inventory_failed")
		}
		pids[i] = uint32(list.PIDs[i])
	}
	if list.Count < list.Assigned {
		return pids, list, errIncompleteInventory
	}
	return pids, list, nil
}
func (j *job) contains(h syscall.Handle) bool {
	j.mu.Lock()
	defer j.mu.Unlock()
	if j.closed.Load() {
		return false
	}
	var yes int32
	ok, _, _ := isInJob.Call(uintptr(h), uintptr(j.handle), uintptr(unsafe.Pointer(&yes)))
	return ok != 0 && yes != 0
}

type protocolPacket struct {
	line []byte
	err  error
}
type child struct {
	owner                      *job
	waitHandle                 syscall.Handle
	handle                     syscall.Handle
	pid                        uint32
	stdin, stdout, stderr      *os.File
	packets                    chan protocolPacket
	drained                    chan error
	done                       chan struct{}
	outputDone, diagnosticDone chan struct{}
	exitCode                   uint32
	closeOnce                  sync.Once
	stdinOnce                  sync.Once
	stdinCloseErr              error
}

func (p *child) closeStdin() error {
	p.stdinOnce.Do(func() { p.stdinCloseErr = p.stdin.Close(); p.owner.resources.record("stdin", p.stdinCloseErr) })
	return p.stdinCloseErr
}
func (p *child) close() {
	p.closeOnce.Do(func() {
		_ = p.closeStdin()
		p.owner.resources.record("stdout", p.stdout.Close())
		p.owner.resources.record("stderr", p.stderr.Close())
		p.owner.resources.record("root_handle", syscall.CloseHandle(p.handle))
	})
}
func (p *child) alive() bool {
	select {
	case <-p.done:
		return false
	default:
		return true
	}
}

// Pipes are read by goroutines. Windows anonymous pipes cannot use Python selectors.
func (p *child) read() {
	go func() {
		defer close(p.outputDone)
		defer close(p.packets)
		r := bufio.NewReaderSize(p.stdout, maxProtocolLine+1)
		for n := 0; ; n++ {
			line, e := r.ReadSlice('\n')
			if e == io.EOF && len(line) == 0 {
				return
			}
			if e != nil || len(line) > maxProtocolLine || n >= 2 {
				p.packets <- protocolPacket{err: failure("bounded_protocol_failed")}
				return
			}
			p.packets <- protocolPacket{line: append([]byte(nil), line...)}
		}
	}()
	go func() { // Discard diagnostics; neither errors nor secret-looking output is persisted.
		defer close(p.diagnosticDone)
		n, e := io.Copy(io.Discard, io.LimitReader(p.stderr, 65537))
		if n != 0 || e != nil {
			p.drained <- failure("unexpected_child_stderr")
		} else {
			p.drained <- nil
		}
	}()
	go func() {
		defer close(p.done)
		defer func() { p.owner.resources.record("wait_handle", syscall.CloseHandle(p.waitHandle)) }()
		_, e := syscall.WaitForSingleObject(p.waitHandle, syscall.INFINITE)
		if e != nil || syscall.GetExitCodeProcess(p.waitHandle, &p.exitCode) != nil {
			p.exitCode = 0xffffffff
		}
	}()
}

type startupInfoEx struct {
	Startup    syscall.StartupInfo
	Attributes uintptr
}

func privatePipe() (*os.File, *os.File, error) {
	var read, write syscall.Handle
	// os.Pipe on Windows creates inheritable endpoints. A nil security
	// attribute starts both endpoints noninheritable instead.
	if err := syscall.CreatePipe(&read, &write, nil, 0); err != nil {
		return nil, nil, err
	}
	return os.NewFile(uintptr(read), "signature-pipe"), os.NewFile(uintptr(write), "signature-pipe"), nil
}

func (j *job) start(path string, args, env []string, dir string) (*child, error) {
	// Handles are created non-inheritable and only the child ends are explicitly
	// permitted by PROC_THREAD_ATTRIBUTE_HANDLE_LIST. No outer-job handle leaks.
	inR, inW, e := privatePipe()
	if e != nil {
		return nil, failure("pipe_create_failed")
	}
	outR, outW, e := privatePipe()
	if e != nil {
		j.resources.record("launch_pipe", inR.Close())
		j.resources.record("launch_pipe", inW.Close())
		return nil, failure("pipe_create_failed")
	}
	errR, errW, e := privatePipe()
	if e != nil {
		j.resources.record("launch_pipe", inR.Close())
		j.resources.record("launch_pipe", inW.Close())
		j.resources.record("launch_pipe", outR.Close())
		j.resources.record("launch_pipe", outW.Close())
		return nil, failure("pipe_create_failed")
	}
	success := false
	defer func() {
		for _, f := range []*os.File{inR, outW, errW} {
			j.resources.record("launch_pipe", f.Close())
		}
		if !success {
			for _, f := range []*os.File{inW, outR, errR} {
				j.resources.record("launch_pipe", f.Close())
			}
		}
	}()
	handles := []syscall.Handle{syscall.Handle(inR.Fd()), syscall.Handle(outW.Fd()), syscall.Handle(errW.Fd())}
	for _, h := range handles {
		if syscall.SetHandleInformation(h, syscall.HANDLE_FLAG_INHERIT, syscall.HANDLE_FLAG_INHERIT) != nil {
			return nil, failure("pipe_inheritance_failed")
		}
	}
	var size uintptr
	initAttributes.Call(0, 2, 0, uintptr(unsafe.Pointer(&size)))
	if size == 0 || size > 65536 {
		return nil, failure("handle_list_failed")
	}
	storage := make([]byte, size)
	attrs := uintptr(unsafe.Pointer(&storage[0]))
	ok, _, _ := initAttributes.Call(attrs, 2, 0, uintptr(unsafe.Pointer(&size)))
	if ok == 0 {
		return nil, failure("handle_list_failed")
	}
	jobs := []syscall.Handle{j.handle}
	defer func() {
		deleteAttributes.Call(attrs)
		runtime.KeepAlive(storage)
		runtime.KeepAlive(handles)
		runtime.KeepAlive(jobs)
	}()
	ok, _, _ = updateAttributes.Call(attrs, 0, 0x20002, uintptr(unsafe.Pointer(&handles[0])), uintptr(len(handles))*unsafe.Sizeof(handles[0]), 0, 0)
	if ok == 0 {
		return nil, failure("handle_list_failed")
	}
	// PROC_THREAD_ATTRIBUTE_JOB_LIST assigns containment atomically with process
	// creation. A parent crash cannot strand a created-but-unassigned suspended
	// child. Windows Server 2022 supports this Windows 10/Server 2016 API.
	ok, _, _ = updateAttributes.Call(attrs, 0, 0x2000D, uintptr(unsafe.Pointer(&jobs[0])), uintptr(len(jobs))*unsafe.Sizeof(jobs[0]), 0, 0)
	if ok == 0 {
		return nil, failure("atomic_job_attribute_failed")
	}
	si := startupInfoEx{Attributes: attrs}
	si.Startup.Cb = uint32(unsafe.Sizeof(si))
	si.Startup.Flags = syscall.STARTF_USESTDHANDLES
	si.Startup.StdInput = handles[0]
	si.Startup.StdOutput = handles[1]
	si.Startup.StdErr = handles[2]
	line := syscall.EscapeArg(path)
	for _, a := range args {
		line += " " + syscall.EscapeArg(a)
	}
	cmd, e := syscall.UTF16PtrFromString(line)
	if e != nil {
		return nil, failure("process_arguments_failed")
	}
	exe, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, failure("process_arguments_failed")
	}
	cwd, e := syscall.UTF16PtrFromString(dir)
	if e != nil {
		return nil, failure("process_arguments_failed")
	}
	env = append([]string(nil), env...)
	sort.Slice(env, func(i, k int) bool { return strings.ToUpper(env[i]) < strings.ToUpper(env[k]) })
	var block []uint16
	for _, s := range env {
		u, e := syscall.UTF16FromString(s)
		if e != nil {
			return nil, failure("process_environment_failed")
		}
		block = append(block, u...)
	}
	block = append(block, 0)
	var pi syscall.ProcessInformation
	// Signature-only CREATE_SUSPENDED + CREATE_UNICODE_ENVIRONMENT +
	// EXTENDED_STARTUPINFO_PRESENT + CREATE_NO_WINDOW. The one exact installed
	// conhost is required and is never an exemption for unknown descendants.
	flags := uint32(0x4 | 0x400 | 0x80000 | 0x08000000)
	j.mu.Lock()
	if j.closed.Load() {
		j.mu.Unlock()
		return nil, failure("safety_job_closed")
	}
	e = syscall.CreateProcess(exe, cmd, nil, nil, true, flags, &block[0], cwd, &si.Startup, &pi)
	j.mu.Unlock()
	runtime.KeepAlive(storage)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(block)
	if e != nil {
		return nil, failure("suspended_launch_failed")
	}
	defer func() { j.resources.record("launch_thread", syscall.CloseHandle(pi.Thread)) }()
	failed := func(code string) (*child, error) {
		_ = syscall.TerminateProcess(pi.Process, 2)
		state, e := syscall.WaitForSingleObject(pi.Process, 3000)
		if e != nil || state != syscall.WAIT_OBJECT_0 {
			j.launchCleanupFailed = true
			code = "signature_start_cleanup_join_failed"
		}
		j.resources.record("launch_process", syscall.CloseHandle(pi.Process))
		return nil, failure(code)
	}
	if !j.contains(pi.Process) {
		return failed("atomic_job_membership_failed")
	}
	if j.beforeResume != nil {
		j.beforeResume(pi.ProcessId)
	}
	if j.closed.Load() {
		return failed("suspended_validation_failed")
	}
	current, e := syscall.GetCurrentProcess()
	if e != nil {
		return failed("process_handle_failed")
	}
	var waitHandle syscall.Handle
	if syscall.DuplicateHandle(current, pi.Process, current, &waitHandle, 0, false, syscall.DUPLICATE_SAME_ACCESS) != nil {
		return failed("process_wait_handle_failed")
	}
	resumed, _, _ := resumeThread.Call(uintptr(pi.Thread))
	if resumed != 1 {
		j.resources.record("wait_handle", syscall.CloseHandle(waitHandle))
		return failed("child_resume_failed")
	}
	p := &child{owner: j, handle: pi.Process, waitHandle: waitHandle, pid: pi.ProcessId, stdin: inW, stdout: outR, stderr: errR, packets: make(chan protocolPacket, 4), drained: make(chan error, 1), done: make(chan struct{}), outputDone: make(chan struct{}), diagnosticDone: make(chan struct{})}
	success = true
	p.read()
	return p, nil
}
func processPath(pid uint32) (string, syscall.Handle, error) {
	h, e := syscall.OpenProcess(0x1000|syscall.SYNCHRONIZE, false, pid)
	if e != nil {
		return "", 0, failure("owned_process_open_failed")
	}
	var name [32768]uint16
	n := uint32(len(name))
	ok, _, _ := imageName.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&name[0])), uintptr(unsafe.Pointer(&n)))
	if ok == 0 || n == 0 {
		return "", 0, closeFailure(failure("owned_process_path_failed"), "inspection_handle", syscall.CloseHandle(h))
	}
	return syscall.UTF16ToString(name[:n]), h, nil
}

// Deny replacement/deletion for the entire native run, including the fixture.
func lockFile(path string) (*os.File, error) {
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() || info.Mode()&os.ModeSymlink != 0 {
		return nil, failure("nonregular_component")
	}
	p, e := syscall.UTF16PtrFromString(path)
	if e != nil {
		return nil, failure("component_path_failed")
	}
	h, e := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if e != nil {
		return nil, failure("component_lock_failed")
	}
	f := os.NewFile(uintptr(h), "verified-component")
	after, e := f.Stat()
	if e != nil || !os.SameFile(info, after) {
		return nil, closeFailure(failure("component_identity_changed"), "dependency", f.Close())
	}
	return f, nil
}
