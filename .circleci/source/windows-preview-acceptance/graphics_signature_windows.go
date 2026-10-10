//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func inspectedGraphicsRejection(code, name, path, directory, systemRoot string, declared bool) error {
	err := rejectedGraphicsModule(code, name, path, directory, systemRoot, declared)
	r := err.(*graphicsModuleFailure)
	if !declared && strings.EqualFold(name, "gdiplus.dll") && r.rejection.Location == "windows_side_by_side" {
		inspection := &graphicsOSInspection{}
		if e := inspectGraphicsOSFile(path, systemRoot, inspection); e != nil {
			inspection.Failure = e.Error()
		}
		r.rejection.Inspection = inspection
	}
	// This increment gathers evidence only. Even a Valid OS signature does not
	// change the rejection or make the module accepted.
	return err
}

func finalGraphicsPath(f *os.File) (string, error) {
	var buffer [32768]uint16
	n, _, _ := kernel32.NewProc("GetFinalPathNameByHandleW").Call(f.Fd(), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0)
	if n == 0 || n >= uintptr(len(buffer)) {
		return "", failure("os_module_final_path_failed")
	}
	return graphicsPath(syscall.UTF16ToString(buffer[:n])), nil
}

func inspectGraphicsOSFile(path, systemRoot string, r *graphicsOSInspection) (result error) {
	f, err := lockFile(path)
	if err != nil {
		return failure("os_module_lock_failed")
	}
	defer f.Close()
	final, err := finalGraphicsPath(f)
	if err != nil {
		return err
	}
	if !strings.EqualFold(final, graphicsPath(path)) || !strings.EqualFold(filepath.Base(final), "gdiplus.dll") ||
		!strings.HasPrefix(strings.ToLower(final), strings.ToLower(filepath.Join(systemRoot, "WinSxS"))+string(filepath.Separator)) {
		return failure("os_module_final_path_mismatch")
	}
	r.FinalPathMatches = true
	r.FileSHA, err = fileSHA(f, 64<<20)
	if err != nil {
		return failure("os_module_hash_failed")
	}
	return runGraphicsSignature(final, systemRoot, r)
}

func runGraphicsSignature(final, systemRoot string, r *graphicsOSInspection) (result error) {
	started := time.Now()
	defer func() { r.ElapsedMillis = time.Since(started).Milliseconds() }()
	work, err := os.MkdirTemp("", "owned-signature-")
	if err != nil {
		return failure("os_verifier_work_failed")
	}
	defer os.Remove(work)
	j, err := newJob()
	if err != nil {
		return err
	}
	defer j.close()
	watchdog := time.AfterFunc(7*time.Second, j.close)
	defer watchdog.Stop()
	p, err := j.start(filepath.Join(systemRoot, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), graphicsSignatureArguments(), childEnvironment(systemRoot, work), work)
	if err != nil {
		return failure("os_verifier_start_failed")
	}
	defer func() {
		select {
		case <-p.done:
			value := p.exitCode
			r.ExitCode = &value
		default:
		}
		if ids, e := j.pids(); j.closed.Load() || e != nil || len(ids) != 0 || p.alive() {
			r.NaturalCleanup = false
			j.close()
			_ = p.wait(time.Second)
			if result == nil {
				result = failure("os_verifier_cleanup_failed")
			}
		}
		p.close()
	}()
	if err := writePrivate(p.stdin, map[string]string{"path": final}); err != nil {
		return failure("os_verifier_input_failed")
	}
	_ = p.stdin.Close()
	raw, err := p.next(5 * time.Second)
	if err != nil {
		switch err.Error() {
		case "protocol_timeout", "protocol_ended_early", "bounded_protocol_failed":
			r.ResultFailure = err.Error()
		default:
			r.ResultFailure = "other_protocol_failure"
		}
		return failure("os_verifier_result_failed")
	}
	defer clear(raw)
	if err = p.wait(time.Second); err != nil {
		return failure("os_verifier_exit_failed")
	}
	if err = p.finishProtocol(); err != nil {
		return failure("os_verifier_protocol_failed")
	}
	if err = waitRetiredInventory(j.pids, map[uint32]bool{p.pid: true}, time.Second); err != nil {
		return failure("os_verifier_inventory_failed")
	}
	r.NaturalCleanup = true
	r.Signature, err = parseGraphicsSignature(raw, r.FileSHA)
	return err
}
