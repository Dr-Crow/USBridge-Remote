//go:build windows

// SPDX-License-Identifier: GPL-3.0-only
package main

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
	"time"
	"unsafe"
)

var kernel32 = syscall.NewLazyDLL("kernel32.dll")
var user32 = syscall.NewLazyDLL("user32.dll")
var gdi32 = syscall.NewLazyDLL("gdi32.dll")
var createJob = kernel32.NewProc("CreateJobObjectW")
var setJob = kernel32.NewProc("SetInformationJobObject")
var queryJob = kernel32.NewProc("QueryInformationJobObject")
var isInJob = kernel32.NewProc("IsProcessInJob")
var resumeThread = kernel32.NewProc("ResumeThread")
var initAttributes = kernel32.NewProc("InitializeProcThreadAttributeList")
var updateAttributes = kernel32.NewProc("UpdateProcThreadAttribute")
var deleteAttributes = kernel32.NewProc("DeleteProcThreadAttributeList")
var imageName = kernel32.NewProc("QueryFullProcessImageNameW")
var enumWindows = user32.NewProc("EnumWindows")
var windowPID = user32.NewProc("GetWindowThreadProcessId")
var windowVisible = user32.NewProc("IsWindowVisible")
var windowText = user32.NewProc("GetWindowTextW")
var getClientRect = user32.NewProc("GetClientRect")
var printWindow = user32.NewProc("PrintWindow")
var postMessage = user32.NewProc("PostMessageW")
var createDC = gdi32.NewProc("CreateCompatibleDC")
var createDIB = gdi32.NewProc("CreateDIBSection")
var selectObject = gdi32.NewProc("SelectObject")
var deleteObject = gdi32.NewProc("DeleteObject")
var deleteDC = gdi32.NewProc("DeleteDC")
var gdiFlush = gdi32.NewProc("GdiFlush")

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
	handle syscall.Handle
	once   sync.Once
	closed atomic.Bool
	// CI test seam only; never set by runtime flags or descriptors.
	beforeResume func(uint32)
}

func newJob() (*job, error) {
	h, _, _ := createJob.Call(0, 0)
	if h == 0 {
		return nil, failure("job_create_failed")
	}
	j := &job{handle: syscall.Handle(h)}
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
		j.closed.Store(true)
		_ = syscall.CloseHandle(j.handle)
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
	var list jobProcessList
	ok, _, _ := queryJob.Call(uintptr(j.handle), 3, uintptr(unsafe.Pointer(&list)), unsafe.Sizeof(list), 0)
	if ok == 0 || list.Count > 128 || list.Assigned != list.Count {
		return nil, list, failure("job_inventory_failed")
	}
	pids := make([]uint32, list.Count)
	for i := range pids {
		if list.PIDs[i] == 0 || list.PIDs[i] > 0xffffffff {
			return nil, list, failure("job_inventory_failed")
		}
		pids[i] = uint32(list.PIDs[i])
	}
	return pids, list, nil
}
func (j *job) contains(h syscall.Handle) bool {
	var yes int32
	ok, _, _ := isInJob.Call(uintptr(h), uintptr(j.handle), uintptr(unsafe.Pointer(&yes)))
	return ok != 0 && yes != 0
}

type protocolPacket struct {
	line []byte
	err  error
}
type child struct {
	owner                 *job
	waitHandle            syscall.Handle
	handle                syscall.Handle
	pid                   uint32
	stdin, stdout, stderr *os.File
	packets               chan protocolPacket
	drained               chan error
	done                  chan struct{}
	exitCode              uint32
	closeOnce             sync.Once
}

func (p *child) close() {
	p.closeOnce.Do(func() {
		_ = p.stdin.Close()
		_ = p.stdout.Close()
		_ = p.stderr.Close()
		_ = syscall.CloseHandle(p.handle)
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
func (p *child) wait(timeout time.Duration) error {
	select {
	case <-p.done:
		return naturalChildExit(p.exitCode, p.owner == nil || p.owner.closed.Load())
	case <-time.After(timeout):
		return failure("natural_exit_timeout")
	}
}
func (p *child) next(timeout time.Duration) ([]byte, error) {
	select {
	case v, ok := <-p.packets:
		if !ok {
			return nil, failure("protocol_ended_early")
		}
		return v.line, v.err
	case <-time.After(timeout):
		return nil, failure("protocol_timeout")
	}
}
func (p *child) finishProtocol() error {
	select {
	case packet, ok := <-p.packets:
		if ok {
			clear(packet.line)
			return failure("extra_child_output")
		}
	case <-time.After(2 * time.Second):
		return failure("protocol_eof_timeout")
	}
	select {
	case e := <-p.drained:
		return e
	case <-time.After(2 * time.Second):
		return failure("stderr_eof_timeout")
	}
}

// Pipes are read by goroutines. Windows anonymous pipes cannot use Python selectors.
func (p *child) read() {
	go func() {
		defer close(p.packets)
		r := bufio.NewReaderSize(p.stdout, maxProtocolLine+1)
		for n := 0; ; n++ {
			line, e := r.ReadSlice('\n')
			if e == io.EOF && len(line) == 0 {
				return
			}
			if e != nil || len(line) > maxProtocolLine || n >= 3 {
				p.packets <- protocolPacket{err: failure("bounded_protocol_failed")}
				return
			}
			p.packets <- protocolPacket{line: append([]byte(nil), line...)}
		}
	}()
	go func() { // Discard diagnostics; neither errors nor secret-looking output is persisted.
		n, e := io.Copy(io.Discard, io.LimitReader(p.stderr, 65537))
		if n != 0 || e != nil {
			p.drained <- failure("unexpected_child_stderr")
		} else {
			p.drained <- nil
		}
	}()
	go func() {
		defer syscall.CloseHandle(p.waitHandle)
		_, e := syscall.WaitForSingleObject(p.waitHandle, syscall.INFINITE)
		if e != nil || syscall.GetExitCodeProcess(p.waitHandle, &p.exitCode) != nil {
			p.exitCode = 0xffffffff
		}
		close(p.done)
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
	return os.NewFile(uintptr(read), "preview-pipe"), os.NewFile(uintptr(write), "preview-pipe"), nil
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
		inR.Close()
		inW.Close()
		return nil, failure("pipe_create_failed")
	}
	errR, errW, e := privatePipe()
	if e != nil {
		inR.Close()
		inW.Close()
		outR.Close()
		outW.Close()
		return nil, failure("pipe_create_failed")
	}
	all := []*os.File{inR, inW, outR, outW, errR, errW}
	success := false
	defer func() {
		inR.Close()
		outW.Close()
		errW.Close()
		if !success {
			for _, f := range all {
				f.Close()
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
	// CREATE_SUSPENDED + CREATE_UNICODE_ENVIRONMENT + EXTENDED_STARTUPINFO_PRESENT
	// + DETACHED_PROCESS. Explicit private pipes do not need a console host.
	// CREATE_NO_WINDOW still creates a conhost.exe member, observed in the native
	// regression; do not hide or allow-list that extra process. The GUI viewer
	// still creates its own normal Fyne HWND.
	e = syscall.CreateProcess(exe, cmd, nil, nil, true, 0x4|0x400|0x80000|0x8, &block[0], cwd, &si.Startup, &pi)
	runtime.KeepAlive(storage)
	runtime.KeepAlive(handles)
	runtime.KeepAlive(block)
	if e != nil {
		return nil, failure("suspended_launch_failed")
	}
	defer syscall.CloseHandle(pi.Thread)
	failed := func(code string) (*child, error) {
		_ = syscall.TerminateProcess(pi.Process, 2)
		_, _ = syscall.WaitForSingleObject(pi.Process, 3000)
		_ = syscall.CloseHandle(pi.Process)
		return nil, failure(code)
	}
	if !j.contains(pi.Process) {
		return failed("atomic_job_membership_failed")
	}
	if j.beforeResume != nil {
		j.beforeResume(pi.ProcessId)
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
		syscall.CloseHandle(waitHandle)
		return failed("child_resume_failed")
	}
	p := &child{owner: j, handle: pi.Process, waitHandle: waitHandle, pid: pi.ProcessId, stdin: inW, stdout: outR, stderr: errR, packets: make(chan protocolPacket, 4), drained: make(chan error, 1), done: make(chan struct{})}
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
		syscall.CloseHandle(h)
		return "", 0, failure("owned_process_path_failed")
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
		f.Close()
		return nil, failure("component_identity_changed")
	}
	return f, nil
}
func verifyWindow(hwnd uintptr, pid uint32) bool {
	var actual uint32
	windowPID.Call(hwnd, uintptr(unsafe.Pointer(&actual)))
	if actual != pid {
		return false
	}
	var title [256]uint16
	n, _, _ := windowText.Call(hwnd, uintptr(unsafe.Pointer(&title[0])), uintptr(len(title)))
	return n > 0 && n < uintptr(len(title)-1) && syscall.UTF16ToString(title[:n]) == viewerTitle
}
func ownedWindow(pid uint32) (uintptr, error) {
	var found uintptr
	ambiguous := false
	callback := syscall.NewCallback(func(hwnd, lparam uintptr) uintptr {
		var owner uint32
		windowPID.Call(hwnd, uintptr(unsafe.Pointer(&owner)))
		if owner != pid {
			return 1
		}
		visible, _, _ := windowVisible.Call(hwnd)
		if visible == 0 || !verifyWindow(hwnd, pid) {
			return 1
		}
		if found != 0 {
			ambiguous = true
		} else {
			found = hwnd
		}
		return 1
	})
	ok, _, _ := enumWindows.Call(callback, 0)
	if ok == 0 || ambiguous {
		return 0, failure("owned_window_ambiguous")
	}
	return found, nil
}
func closeWindow(hwnd uintptr, pid uint32) error {
	if !verifyWindow(hwnd, pid) {
		return failure("owned_window_identity_changed")
	}
	ok, _, _ := postMessage.Call(hwnd, 0x10, 0, 0)
	if ok == 0 {
		return failure("owned_window_close_failed")
	}
	return nil
}

type rect struct{ Left, Top, Right, Bottom int32 }
type bitmapHeader struct {
	Size                   uint32
	Width, Height          int32
	Planes, BitCount       uint16
	Compression, SizeImage uint32
	XPels, YPels           int32
	ClrUsed, ClrImportant  uint32
}

func printOwnedWindow(hwnd uintptr, pid uint32) (int, error) {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	// No GetDC, BitBlt, desktop/window-manager screen capture or screen fallback.
	// CreateCompatibleDC(NULL) creates only a memory DC. Its only pixel source is
	// PrintWindow called on the exact owned HWND after identity revalidation.
	if !verifyWindow(hwnd, pid) {
		return 0, failure("owned_window_identity_changed")
	}
	var r rect
	ok, _, _ := getClientRect.Call(hwnd, uintptr(unsafe.Pointer(&r)))
	w, h := int(r.Right-r.Left), int(r.Bottom-r.Top)
	if ok == 0 || w < 640 || h < 360 || w > 2048 || h > 1536 {
		return 0, failure("owned_window_dimensions")
	}
	dc, _, _ := createDC.Call(0)
	if dc == 0 {
		return 0, failure("memory_dc_failed")
	}
	defer deleteDC.Call(dc)
	header := bitmapHeader{Size: 40, Width: int32(w), Height: -int32(h), Planes: 1, BitCount: 32}
	var bits unsafe.Pointer
	dib, _, _ := createDIB.Call(dc, uintptr(unsafe.Pointer(&header)), 0, uintptr(unsafe.Pointer(&bits)), 0, 0)
	if dib == 0 || bits == nil {
		return 0, failure("owned_dib_failed")
	}
	defer deleteObject.Call(dib)
	old, _, _ := selectObject.Call(dc, dib)
	if old == 0 || old == ^uintptr(0) {
		return 0, failure("owned_dib_select_failed")
	}
	defer selectObject.Call(dc, old)
	pixels := unsafe.Slice((*byte)(bits), w*h*4)
	clear(pixels)
	ok, _, _ = printWindow.Call(hwnd, dc, 0x1|0x2)
	if ok == 0 {
		return 0, failure("printwindow_unavailable")
	}
	flushed, _, _ := gdiFlush.Call()
	if flushed == 0 {
		return 0, failure("owned_dib_flush_failed")
	}
	if !verifyWindow(hwnd, pid) {
		return 0, failure("owned_window_identity_changed")
	}
	counts := [3]int{}
	for y := h * 2 / 5; y < h*3/5; y += 7 {
		for x := w * 2 / 5; x < w*3/5; x += 7 {
			i := (y*w + x) * 4
			counts[pixelColor(pixels[i], pixels[i+1], pixels[i+2])]++
		}
	}
	total := counts[0] + counts[1] + counts[2]
	if total < 16 {
		return 0, failure("owned_pixels_missing")
	}
	for color := 1; color <= 2; color++ {
		if counts[color]*100 >= total*85 {
			return color, nil
		}
	}
	return 0, nil
}
func boundedPixels(hwnd uintptr, pid uint32) (int, error) {
	type result struct {
		color int
		err   error
	}
	ch := make(chan result, 1)
	go func() { c, e := printOwnedWindow(hwnd, pid); ch <- result{c, e} }()
	select {
	case r := <-ch:
		return r.color, r.err
	case <-time.After(2 * time.Second):
		return 0, failure("printwindow_timeout")
	}
}
