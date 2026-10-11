//go:build windows

package main

import (
	"context"
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
	ctx, cancel := context.WithDeadline(context.Background(), started.Add(signatureOwnerBudget))
	defer cancel()
	var console *signatureConsoleOwner
	startupRecorded := false
	defer func() {
		if !startupRecorded {
			r.StartupElapsedMillis = time.Since(started).Milliseconds()
		}
		r.ElapsedMillis = time.Since(started).Milliseconds()
		if signatureBudgetErr(ctx) != nil {
			if console != nil {
				console.recordTimeout(r)
			} else {
				r.TimedOut = true
			}
			r.NaturalCleanup = false
			if result == nil {
				result = failure("signature_owner_timeout")
			}
		}
	}()
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
	console, err = prepareSignatureConsole(systemRoot)
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
	// Prelaunch file/hash preparation consumes the same absolute run budget.
	if signatureBudgetErr(ctx) != nil {
		return failure("signature_owner_timeout")
	}
	j, err := newJob()
	if err != nil {
		return err
	}
	defer j.close()
	j.signatureConsole = true
	var suspendedErr error
	j.beforeResume = func(pid uint32) {
		if signatureBudgetErr(ctx) != nil {
			suspendedErr = failure("signature_owner_timeout")
			j.close()
			return
		}
		suspendedErr = console.beforeResume(j, pid)
		if signatureBudgetErr(ctx) != nil {
			suspendedErr = failure("signature_owner_timeout")
		}
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
		// This watchdog shares the function-entry deadline with every stage.
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
		<-watchDone // the safety callback cannot race success
		if signatureBudgetErr(ctx) != nil {
			console.recordTimeout(r)
			r.NaturalCleanup = false
			if result == nil {
				result = failure("signature_owner_timeout")
			}
		}
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
			waitMillis := uint32(0)
			if result != nil {
				waitMillis = 1000 // failure-only cleanup grace
			}
			state, e := syscall.WaitForSingleObject(console.handle, waitMillis)
			joined = e == nil && state == syscall.WAIT_OBJECT_0
		}
		joinCtx := ctx
		if result != nil {
			var stopCleanup context.CancelFunc
			joinCtx, stopCleanup = context.WithTimeout(context.Background(), time.Second)
			defer stopCleanup()
		}
		workersJoined := signatureJoinWorkers(joinCtx, inputDone, p.done, p.outputDone, p.diagnosticDone)
		if !workersJoined && result == nil && signatureBudgetErr(ctx) != nil {
			// Once success misses its deadline, only bounded failure cleanup may
			// continue. It cannot turn the result back into success.
			result = failure("signature_owner_timeout")
			console.recordTimeout(r)
			r.NaturalCleanup = false
			cleanupCtx, stopCleanup := context.WithTimeout(context.Background(), time.Second)
			workersJoined = signatureJoinWorkers(cleanupCtx, inputDone, p.done, p.outputDone, p.diagnosticDone)
			stopCleanup()
		}
		joined = joined && workersJoined
		r.CleanupJoined = joined
		if !joined {
			r.NaturalCleanup = false
			r.CleanupFailure = "signature_cleanup_join_failed"
			if result == nil {
				result = failure(r.CleanupFailure)
			}
		}
	}()
	args, env := graphicsSignatureArguments(scriptPath), childEnvironment(systemRoot, work)
	if signatureBudgetErr(ctx) != nil {
		return failure("signature_owner_timeout")
	}
	p, err = j.start(powerPath, args, env, work)
	if suspendedErr != nil {
		return suspendedErr
	}
	if err != nil {
		return failure("os_verifier_start_failed")
	}
	err = console.startup(ctx, j, p, r)
	r.StartupElapsedMillis = time.Since(started).Milliseconds()
	startupRecorded = true
	if err != nil {
		return err
	}
	inputStarted := time.Now()
	err = func() error {
		defer func() { r.InputElapsedMillis = time.Since(inputStarted).Milliseconds() }()
		inputCtx, stopInput := context.WithTimeout(ctx, 3*time.Second)
		defer stopInput()
		input, e := json.Marshal(map[string]string{"path": final})
		if e != nil {
			return failure("os_verifier_input_failed")
		}
		input = append(input, '\n')
		if signatureBudgetErr(inputCtx) != nil {
			clear(input)
			console.recordTimeout(r)
			return failure("os_verifier_input_timeout")
		}
		inputDone = make(chan struct{})
		inputResult := make(chan error, 1)
		go func() { defer close(inputDone); defer clear(input); _, e := p.stdin.Write(input); inputResult <- e }()
		writeErr, _, budgetErr := signatureReceive(inputCtx, inputResult)
		_ = p.stdin.Close()
		if budgetErr != nil || signatureBudgetErr(inputCtx) != nil {
			console.recordTimeout(r)
			return failure("os_verifier_input_timeout")
		}
		if writeErr != nil {
			return failure("os_verifier_input_failed")
		}
		return nil
	}()
	if err != nil {
		return err
	}
	queryStarted := time.Now()
	raw, err := console.collect(ctx, j, p, r)
	r.QueryElapsedMillis = time.Since(queryStarted).Milliseconds()
	defer clear(raw)
	if err != nil {
		r.ResultFailure = err.Error()
		return failure("os_verifier_result_failed")
	}
	r.Signature, err = parseGraphicsSignature(raw, r.FileSHA)
	return err
}
