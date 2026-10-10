//go:build windows && signatureprobe

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

type powershellLiteralResult struct {
	Role               string  `json:"role"`
	SHA                string  `json:"executable_sha256,omitempty"`
	Present            bool    `json:"present"`
	Matched            bool    `json:"literal_matched"`
	Sentinel           bool    `json:"script_sentinel_written"`
	SignaturePreflight bool    `json:"signature_preflight_verified"`
	Natural            bool    `json:"natural_cleanup"`
	Safety             bool    `json:"safety_job_closed"`
	Failure            string  `json:"failure_code,omitempty"`
	ExitCode           *uint32 `json:"exit_code,omitempty"`
	Millis             int64   `json:"elapsed_ms"`
}

func powershellLiteral(path, pin, root string, r *powershellLiteralResult) (result error) {
	start := time.Now()
	defer func() { r.Millis = time.Since(start).Milliseconds() }()
	f, err := lockFile(path)
	if err != nil {
		return failure("literal_executable_lock_failed")
	}
	defer f.Close()
	r.Present = true
	r.SHA, err = fileSHA(f, 64<<20)
	if err != nil {
		return failure("literal_executable_hash_failed")
	}
	if pin != "" && r.SHA != pin {
		return failure("literal_executable_pin_mismatch")
	}
	final, err := finalGraphicsPath(f)
	if err != nil || !strings.EqualFold(final, graphicsPath(path)) {
		return failure("literal_executable_final_path_failed")
	}
	work, err := os.MkdirTemp("", "owned-literal-")
	if err != nil {
		return failure("literal_work_failed")
	}
	defer os.Remove(work)
	script := filepath.Join(work, "literal.ps1")
	if os.WriteFile(script, []byte("[IO.File]::WriteAllText((Join-Path $PSScriptRoot 'sentinel.txt'),'signature_literal_v1')\r\nWrite-Output 'signature_literal_v1'\r\nexit 0\r\n"), 0600) != nil {
		return failure("literal_script_failed")
	}
	defer os.Remove(script)
	marker := filepath.Join(work, "sentinel.txt")
	defer os.Remove(marker)
	scriptPin, err := lockFile(script)
	if err != nil {
		return err
	}
	defer scriptPin.Close()
	j, err := newJob()
	if err != nil {
		return err
	}
	defer j.close()
	watchdog := time.AfterFunc(7*time.Second, j.close)
	defer watchdog.Stop()
	p, err := j.start(path, graphicsSignatureArguments(script), childEnvironment(root, work), work)
	if err != nil {
		return err
	}
	defer func() {
		if ids, e := j.pids(); j.closed.Load() || e != nil || len(ids) != 0 || p.alive() {
			r.Safety = true
			r.Natural = false
			j.close()
			_ = p.wait(time.Second)
			if result == nil {
				result = failure("literal_cleanup_failed")
			}
		}
		select {
		case <-p.done:
			exit := p.exitCode
			r.ExitCode = &exit
		default:
		}
		p.close()
	}()
	_ = p.stdin.Close()
	raw, readErr := p.next(5 * time.Second)
	defer clear(raw)
	if readErr == nil {
		r.Matched = string(raw) == "signature_literal_v1\r\n" || string(raw) == "signature_literal_v1\n"
	}
	if e := p.wait(time.Second); e != nil {
		return e
	}
	if info, e := os.Lstat(marker); e == nil && info.Mode().IsRegular() && info.Size() == 20 {
		b, e := os.ReadFile(marker)
		r.Sentinel = e == nil && string(b) == "signature_literal_v1"
	}
	if e := p.finishProtocol(); e != nil {
		return e
	}
	if e := waitRetiredInventory(j.pids, map[uint32]bool{p.pid: true}, time.Second); e != nil {
		return e
	}
	r.Natural = true
	if readErr != nil {
		switch readErr.Error() {
		case "protocol_ended_early", "protocol_timeout", "bounded_protocol_failed":
			return readErr
		default:
			return failure("literal_protocol_failed")
		}
	}
	if !r.Matched || !r.Sentinel {
		return failure("literal_mismatch")
	}
	return nil
}

func TestWindowsPowerShellDetachedLiteral(t *testing.T) {
	out := os.Getenv("WINDOWS_POWERSHELL_LITERAL_RECEIPT")
	root := os.Getenv("SystemRoot")
	commit := os.Getenv("CIRCLE_SHA1")
	if !drivePath.MatchString(out) || !drivePath.MatchString(root) || !commitPattern.MatchString(commit) {
		t.Fatal("literal_configuration_missing")
	}
	if _, e := os.Lstat(out); !os.IsNotExist(e) {
		t.Fatal("literal_receipt_must_be_new")
	}
	results := []powershellLiteralResult{{Role: "windows_powershell_51"}, {Role: "installed_powershell_core"}}
	if err := powershellLiteral(filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), "", root, &results[0]); err != nil {
		results[0].Failure = err.Error()
	}
	core, pin := os.Getenv("WINDOWS_SIGNATURE_CORE_PATH"), os.Getenv("WINDOWS_SIGNATURE_CORE_SHA256")
	if info, e := os.Stat(filepath.Join(os.Getenv("ProgramFiles"), "PowerShell", "7", "pwsh.exe")); e == nil {
		results[1].Present = info.Mode().IsRegular()
	}
	if core != "" || pin != "" {
		if !hashPattern.MatchString(pin) || !strings.EqualFold(core, filepath.Join(os.Getenv("ProgramFiles"), "PowerShell", "7", "pwsh.exe")) {
			t.Fatal("literal_core_identity_invalid")
		}
		results[1].SignaturePreflight = true
		if err := powershellLiteral(core, pin, root, &results[1]); err != nil {
			results[1].Failure = err.Error()
		}
	}
	passed := false
	for _, r := range results {
		passed = passed || (r.Matched && r.Sentinel && r.Natural && !r.Safety && r.Failure == "")
	}
	r := struct {
		Schema  int                       `json:"schema_version"`
		Commit  string                    `json:"commit"`
		Passed  bool                      `json:"passed"`
		Results []powershellLiteralResult `json:"results"`
	}{1, commit, passed, results}
	raw, e := json.MarshalIndent(r, "", "  ")
	if e != nil || writeReceipt(out, append(raw, '\n')) != nil {
		t.Fatal("literal_receipt_failed")
	}
	if !passed {
		t.Fatal("no_detached_powershell_literal_result")
	}
}
