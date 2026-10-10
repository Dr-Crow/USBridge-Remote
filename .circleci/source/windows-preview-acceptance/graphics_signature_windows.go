//go:build windows

package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
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

func runGraphicsSignature(final, systemRoot string, r *graphicsOSInspection) error {
	return runGraphicsSignatureScript(final, systemRoot, r, graphicsSignatureScript)
}

// Only the immutable production constant above is reachable from runtime paths.
// The native prerequisite supplies source-owned negative scripts through tests.
func runGraphicsSignatureScript(final, systemRoot string, r *graphicsOSInspection, scriptText string) (result error) {
	started := time.Now()
	defer func() { r.ElapsedMillis = time.Since(started).Milliseconds() }()
	work, err := os.MkdirTemp("", "owned-signature-")
	if err != nil {
		return failure("os_verifier_work_failed")
	}
	defer os.Remove(work)
	scriptPath := filepath.Join(work, "verify-os-file.ps1")
	if err := os.WriteFile(scriptPath, []byte(scriptText), 0600); err != nil {
		return failure("os_verifier_script_failed")
	}
	defer os.Remove(scriptPath)
	script, err := lockFile(scriptPath)
	if err != nil {
		return failure("os_verifier_script_lock_failed")
	}
	defer script.Close()
	digest := sha256.Sum256([]byte(scriptText))
	r.ScriptSHA = hex.EncodeToString(digest[:])
	if got, e := fileSHA(script, 65536); e != nil || got != r.ScriptSHA {
		return failure("os_verifier_script_hash_failed")
	}
	console, err := prepareSignatureConsole(systemRoot)
	if err != nil {
		return err
	}
	defer console.close()
	powerPath := filepath.Join(filepath.Dir(console.path), "WindowsPowerShell", "v1.0", "powershell.exe")
	powerFile, err := lockFile(powerPath)
	if err != nil {
		return failure("signature_root_lock_failed")
	}
	defer powerFile.Close()
	powerFinal, err := finalGraphicsPath(powerFile)
	if err != nil || !strings.EqualFold(powerFinal, graphicsPath(powerPath)) {
		return failure("signature_root_final_path_failed")
	}
	r.VerifierSHA, err = fileSHA(powerFile, 64<<20)
	if err != nil {
		return failure("signature_root_hash_failed")
	}
	j, err := newJob()
	if err != nil {
		return err
	}
	defer j.close()
	j.signatureConsole = true
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
	watchStop, watchDone := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(watchDone)
		// Actual catalog lookup took 6.946s in native job380. Keep the full
		// startup+query bounded while avoiding a 54ms scheduling margin.
		timer := time.NewTimer(12 * time.Second)
		defer timer.Stop()
		select {
		case <-watchStop:
		case <-timer.C:
			j.close()
		}
	}()
	var p *child
	inputDone := make(chan struct{})
	close(inputDone)
	defer func() {
		close(watchStop)
		<-watchDone // the safety callback cannot race success
		if p == nil {
			j.close()
			r.SafetyJobClosed = true
			joined := console.rootHandle != 0
			for _, h := range []syscall.Handle{console.rootHandle, console.handle} {
				if h != 0 {
					state, e := syscall.WaitForSingleObject(h, 1000)
					joined = joined && e == nil && state == syscall.WAIT_OBJECT_0
				}
			}
			r.CleanupJoined = joined
			if !joined {
				r.CleanupFailure = "signature_cleanup_join_failed"
			}
			return
		}
		if ids, e := j.pids(); j.closed.Load() || e != nil || len(ids) != 0 || p.alive() {
			r.NaturalCleanup = false
			r.SafetyJobClosed = true
			j.close()
			_ = p.wait(time.Second)
			if console.handle != 0 {
				_, _ = syscall.WaitForSingleObject(console.handle, 1000)
			}
			if result == nil {
				result = failure("os_verifier_cleanup_failed")
			}
		}
		select {
		case <-p.done:
			value := p.exitCode
			r.ExitCode = &value
		default:
		}
		p.close()
		joined := true
		if console.handle != 0 {
			state, e := syscall.WaitForSingleObject(console.handle, 1000)
			joined = e == nil && state == syscall.WAIT_OBJECT_0
		}
		deadline := time.NewTimer(time.Second)
		defer deadline.Stop()
	joinLoop:
		for _, done := range []<-chan struct{}{inputDone, p.done, p.outputDone, p.diagnosticDone} {
			select {
			case <-done:
			case <-deadline.C:
				joined = false
				break joinLoop
			}
		}
		r.CleanupJoined = joined
		if !joined {
			r.NaturalCleanup = false
			r.CleanupFailure = "signature_cleanup_join_failed"
			if result == nil {
				result = failure(r.CleanupFailure)
			}
		}
	}()
	p, err = j.start(powerPath, graphicsSignatureArguments(scriptPath), childEnvironment(systemRoot, work), work)
	if suspendedErr != nil {
		return suspendedErr
	}
	if err != nil {
		return failure("os_verifier_start_failed")
	}
	if err = console.startup(j, p, r); err != nil {
		return err
	}
	input, err := json.Marshal(map[string]string{"path": final})
	if err != nil {
		return failure("os_verifier_input_failed")
	}
	input = append(input, '\n')
	inputDone = make(chan struct{})
	inputResult := make(chan error, 1)
	go func() { defer close(inputDone); defer clear(input); _, e := p.stdin.Write(input); inputResult <- e }()
	select {
	case e := <-inputResult:
		if e != nil {
			return failure("os_verifier_input_failed")
		}
	case <-time.After(3 * time.Second):
		_ = p.stdin.Close()
		return failure("os_verifier_input_timeout")
	}

	_ = p.stdin.Close()
	raw, err := console.collect(j, p, r)
	defer clear(raw)
	if err != nil {
		r.ResultFailure = err.Error()
		return failure("os_verifier_result_failed")
	}
	r.Signature, err = parseGraphicsSignature(raw, r.FileSHA)
	return err
}
