//go:build windows && amd64

// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

func graphicsPath(path string) string { return filepath.Clean(strings.TrimPrefix(path, `\\?\`)) }
func finalGraphicsPath(f *os.File) (string, error) {
	var buffer [32768]uint16
	n, _, _ := kernel32.NewProc("GetFinalPathNameByHandleW").Call(f.Fd(), uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)), 0)
	if n == 0 || n >= uintptr(len(buffer)) {
		return "", failure("signature_final_path_failed")
	}
	return graphicsPath(syscall.UTF16ToString(buffer[:n])), nil
}
func childEnvironment(root, work string) []string {
	return []string{"SystemRoot=" + root, "WINDIR=" + root, "PATH=" + filepath.Join(root, "System32") + ";" + root, "TEMP=" + work, "TMP=" + work, "USERPROFILE=" + work, "HOME=" + work, "APPDATA=" + filepath.Join(work, "roaming"), "LOCALAPPDATA=" + filepath.Join(work, "local")}
}
func lockDirectory(path string) (*os.File, error) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return nil, failure("signature_work_invalid")
	}
	ptr, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, failure("signature_work_invalid")
	}
	h, err := syscall.CreateFile(ptr, syscall.GENERIC_READ, syscall.FILE_SHARE_READ|syscall.FILE_SHARE_WRITE, nil, syscall.OPEN_EXISTING, 0x02000000, 0)
	if err != nil {
		return nil, failure("signature_work_lock_failed")
	}
	f := os.NewFile(uintptr(h), "signature-work")
	after, err := f.Stat()
	final, e := finalGraphicsPath(f)
	if err != nil || e != nil || !os.SameFile(info, after) || !strings.EqualFold(final, graphicsPath(path)) {
		return nil, closeFailure(failure("signature_work_identity_failed"), "work", f.Close())
	}
	return f, nil
}
func lockPinned(path, digest string, limit int64) (*os.File, error) {
	f, err := lockFile(path)
	if err != nil {
		return nil, err
	}
	final, err := finalGraphicsPath(f)
	if err != nil || !strings.EqualFold(final, graphicsPath(path)) {
		return nil, closeFailure(failure("signature_pinned_final_path_failed"), "dependency", f.Close())
	}
	got, err := fileSHA(f, limit)
	if err != nil || got != digest {
		return nil, closeFailure(failure("signature_pinned_hash_failed"), "dependency", f.Close())
	}
	return f, nil
}

func run(ctx context.Context, spec Spec, input []byte) (r Result, result error) {
	resources := &resourceTracker{}
	defer resources.finish(&r, &result)
	work, err := lockDirectory(spec.WorkDir)
	if err != nil {
		return r, err
	}
	defer func() { resources.record("work", work.Close()) }()
	script, err := lockPinned(spec.ScriptPath, spec.ScriptSHA256, MaxScriptBytes)
	if err != nil {
		return r, err
	}
	defer func() { resources.record("script", script.Close()) }()
	r.ScriptSHA = spec.ScriptSHA256
	console, err := prepareSignatureConsole(spec.SystemRoot, resources)
	if err != nil {
		return r, err
	}
	defer console.close()
	if !strings.EqualFold(console.path, graphicsPath(spec.ConsoleHostPath)) || console.hash != spec.ConsoleHostSHA256 {
		return r, failure("signature_host_pin_mismatch")
	}
	power, err := lockPinned(spec.PowerShellPath, spec.PowerShellSHA256, 64<<20)
	if err != nil {
		return r, err
	}
	defer func() { resources.record("power", power.Close()) }()
	r.VerifierSHA = spec.PowerShellSHA256
	console.rootFile, console.rootHash = power, spec.PowerShellSHA256
	var dependencies []*os.File
	defer func() {
		for _, f := range dependencies {
			resources.record("dependency", f.Close())
		}
	}()
	for _, dependency := range spec.Dependencies {
		if err = ctx.Err(); err != nil {
			return r, err
		}
		f, e := lockPinned(dependency.Path, dependency.SHA256, MaxDependencyBytes)
		if e != nil {
			return r, e
		}
		dependencies = append(dependencies, f)
	}
	if err = ctx.Err(); err != nil {
		return r, err
	}
	j, err := newJob(resources)
	if err != nil {
		return r, err
	}
	defer j.close()
	var suspendedErr error
	j.beforeResume = func(pid uint32) {
		suspendedErr = console.beforeResume(j, pid)
		r.SuspendedTotal, r.SuspendedActive, r.SuspendedMembers = console.beforeTotal, console.beforeActive, console.beforeCount
		if suspendedErr != nil {
			j.close()
		} else {
			r.SuspendedRootVerified = true
		}
	}
	// This single deadline is created by Run before native preparation. It is never
	// renewed at startup, request write, per-file query, protocol EOF or retirement.
	watchStop, watchDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watchDone)
		select {
		case <-watchStop:
		case <-ctx.Done():
			j.close()
		}
	}()
	var p *child
	inputDone := make(chan struct{})
	close(inputDone)
	defer func() {
		close(watchStop)
		<-watchDone
		r.WatchdogJoined = true
		if ctx.Err() != nil && result == nil {
			result = ctx.Err()
		}
		if p == nil {
			j.close()
			r.SafetyJobClosed = true
			r.CleanupJoined = !j.launchCleanupFailed && joinHandles(time.Now().Add(3*time.Second), console.rootHandle, console.handle)
			if !r.CleanupJoined {
				r.CleanupFailure = "signature_cleanup_join_failed"
			}
			clear(r.Output)
			r.Output = nil
			return
		}
		// Safety closure remains recorded separately from final release of an already
		// empty Job. Even forced exit code zero can never masquerade as success.
		ids, e := j.pids()
		if result != nil || j.closed.Load() || e != nil || len(ids) != 0 || p.alive() {
			r.NaturalCleanup = false
			r.SafetyJobClosed = true
			j.close()
			if result == nil {
				result = failure("signature_cleanup_failed")
			}
		}
		deadline := time.Now().Add(3 * time.Second)
		handlesJoined := joinHandles(deadline, p.handle, console.handle)
		// Close pipe handles to unblock partial output/input on all failure paths.
		p.close()
		joined := joinWorkers(deadline, inputDone, p.done, p.outputDone, p.diagnosticDone)
		r.CleanupJoined = handlesJoined && joined
		if !r.CleanupJoined {
			r.NaturalCleanup = false
			r.CleanupFailure = "signature_cleanup_join_failed"
			if result == nil {
				result = failure(r.CleanupFailure)
			}
		}
		select {
		case <-p.done:
			value := p.exitCode
			r.ExitCode = &value
		default:
		}
		if console.handle != 0 {
			if zero, e := signatureProcessZero(console.handle); e == nil && zero {
				value := uint32(0)
				r.ConsoleHostExitCode = &value
			}
		}
		if ctx.Err() != nil && result == nil {
			result = ctx.Err()
		}
		if result == nil && (!r.NaturalCleanup || !r.CleanupJoined || !r.WatchdogJoined || r.SafetyJobClosed || r.ExitCode == nil || *r.ExitCode != 0 || r.ConsoleHostExitCode == nil || *r.ConsoleHostExitCode != 0) {
			result = failure("signature_cleanup_incomplete")
		}
		if result != nil {
			clear(r.Output)
			r.Output = nil
		}
	}()
	p, err = j.start(spec.PowerShellPath, signatureArguments(spec.ScriptPath), childEnvironment(spec.SystemRoot, spec.WorkDir), spec.WorkDir)
	if suspendedErr != nil {
		return r, suspendedErr
	}
	if err != nil {
		return r, err
	}
	if err = console.startup(ctx, j, p, &r); err != nil {
		return r, err
	}
	inputDone = make(chan struct{})
	inputResult := make(chan error, 1)
	payload := append([]byte(nil), input...)
	go func() {
		defer close(inputDone)
		defer clear(payload)
		n, e := p.stdin.Write(payload)
		if e == nil && n != len(payload) {
			e = failure("signature_input_partial")
		}
		inputResult <- e
	}()
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case e := <-inputResult:
		if e != nil {
			return r, failure("signature_input_failed")
		}
	case <-ctx.Done():
		return r, ctx.Err()
	case <-timer.C:
		return r, failure("signature_input_timeout")
	}
	if err = p.closeStdin(); err != nil {
		return r, &ResourceCloseError{Resources: []string{"stdin"}}
	}
	raw, err := console.collect(ctx, j, p, &r)
	if err != nil {
		clear(raw)
		return r, err
	}
	if err = ctx.Err(); err != nil {
		clear(raw)
		return r, err
	}
	if err = validateOutput(raw); err != nil {
		clear(raw)
		return r, err
	}
	r.Output = raw
	return r, nil
}

func joinHandles(deadline time.Time, handles ...syscall.Handle) bool {
	joined := true
	for _, h := range handles {
		if h == 0 {
			continue
		}
		remaining := time.Until(deadline)
		timeout := uint32(0)
		if remaining > 0 {
			timeout = uint32((remaining + time.Millisecond - 1) / time.Millisecond)
		}
		state, e := syscall.WaitForSingleObject(h, timeout)
		if e != nil || state != syscall.WAIT_OBJECT_0 {
			joined = false
		}
	}
	return joined
}
