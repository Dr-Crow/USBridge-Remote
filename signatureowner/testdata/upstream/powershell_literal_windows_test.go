//go:build windows && signatureprobe

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
)

type powershellLiteralResult struct {
	Role                     string  `json:"role"`
	LaunchMode               string  `json:"launch_mode"`
	StdinLifetime            string  `json:"stdin_lifetime"`
	Environment              string  `json:"environment"`
	MaxOwned                 int     `json:"maximum_observed_owned_processes"`
	ConsoleHosts             int     `json:"observed_system32_console_hosts"`
	OtherMembers             int     `json:"observed_other_members"`
	InventoryFailures        int     `json:"inventory_failures"`
	MemberInspectionFailures int     `json:"member_inspection_failures"`
	SHA                      string  `json:"executable_sha256,omitempty"`
	Present                  bool    `json:"present"`
	Matched                  bool    `json:"literal_matched"`
	Sentinel                 bool    `json:"script_sentinel_written"`
	SignaturePreflight       bool    `json:"signature_preflight_verified"`
	Natural                  bool    `json:"natural_cleanup"`
	Safety                   bool    `json:"safety_job_closed"`
	Failure                  string  `json:"failure_code,omitempty"`
	ExitCode                 *uint32 `json:"exit_code,omitempty"`
	Millis                   int64   `json:"elapsed_ms"`
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
	if os.WriteFile(script, []byte("[IO.File]::WriteAllText((Join-Path $PSScriptRoot 'sentinel.txt'),'signature_literal_v1')\r\nWrite-Output 'signature_literal_v1'\r\nStart-Sleep -Milliseconds 200\r\nexit 0\r\n"), 0600) != nil {
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
	j.signatureConsole = r.LaunchMode == "no_window"
	env := childEnvironment(root, work)
	if r.Environment == "os_expanded" {
		for _, key := range []string{"ComSpec", "SystemDrive", "ProgramFiles", "ProgramFiles(x86)", "ProgramW6432", "CommonProgramFiles", "CommonProgramFiles(x86)", "CommonProgramW6432", "PROCESSOR_ARCHITECTURE", "PROCESSOR_IDENTIFIER", "PROCESSOR_LEVEL", "PROCESSOR_REVISION", "NUMBER_OF_PROCESSORS", "OS"} {
			v := os.Getenv(key)
			if len(v) > 2048 || strings.ContainsAny(v, "\x00\r\n") {
				return failure("literal_os_environment_invalid")
			}
			if v != "" {
				env = append(env, key+"="+v)
			}
		}
	}
	p, err := j.start(path, graphicsSignatureArguments(script), env, work)
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
	// Observe only this Job's members. A console host remains a test failure;
	// no allow-list or acceptance policy is changed by this diagnostic.
	stopSamples, sampled := make(chan struct{}), make(chan struct{})
	go func() {
		defer close(sampled)
		consoleIDs, otherIDs, failedIDs := map[uint32]bool{}, map[uint32]bool{}, map[uint32]bool{}
		ticker := time.NewTicker(10 * time.Millisecond)
		defer ticker.Stop()
		for {
			ids, e := j.pids()
			if e != nil {
				r.InventoryFailures++
			} else {
				if len(ids) > r.MaxOwned {
					r.MaxOwned = len(ids)
				}
				for _, id := range ids {
					if id == p.pid || consoleIDs[id] || otherIDs[id] || failedIDs[id] {
						continue
					}
					name, h, e := processPath(id)
					if e != nil {
						failedIDs[id] = true
						r.MemberInspectionFailures++
						continue
					}
					owned := j.contains(h)
					_ = syscall.CloseHandle(h)
					if !owned {
						failedIDs[id] = true
						r.MemberInspectionFailures++
					} else if strings.EqualFold(graphicsPath(name), graphicsPath(filepath.Join(root, "System32", "conhost.exe"))) {
						consoleIDs[id] = true
						r.ConsoleHosts++
					} else {
						otherIDs[id] = true
						r.OtherMembers++
					}
				}
			}
			select {
			case <-stopSamples:
				return
			case <-ticker.C:
			}
		}
	}()
	defer func() { close(stopSamples); <-sampled }()
	if r.StdinLifetime == "early_eof" {
		_ = p.stdin.Close()
	}
	raw, readErr := p.next(5 * time.Second)
	defer clear(raw)
	_ = p.stdin.Close()
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
	core, pin := os.Getenv("WINDOWS_SIGNATURE_CORE_PATH"), os.Getenv("WINDOWS_SIGNATURE_CORE_SHA256")
	if core != "" || pin != "" {
		if !hashPattern.MatchString(pin) || !strings.EqualFold(core, filepath.Join(os.Getenv("ProgramFiles"), "PowerShell", "7", "pwsh.exe")) {
			t.Fatal("literal_core_identity_invalid")
		}
	}
	var results []powershellLiteralResult
	for _, host := range []struct{ role, path, pin string }{
		{"windows_powershell_51", filepath.Join(root, "System32", "WindowsPowerShell", "v1.0", "powershell.exe"), ""},
		{"installed_powershell_core", core, pin},
	} {
		if host.path == "" {
			results = append(results, powershellLiteralResult{Role: host.role, Failure: "literal_candidate_unavailable"})
			continue
		}
		for _, mode := range []string{"detached", "no_window"} {
			for _, stdin := range []string{"early_eof", "until_output"} {
				for _, environment := range []string{"minimal", "os_expanded"} {
					r := powershellLiteralResult{Role: host.role, LaunchMode: mode, StdinLifetime: stdin, Environment: environment, SignaturePreflight: host.pin != ""}
					if err := powershellLiteral(host.path, host.pin, root, &r); err != nil {
						r.Failure = err.Error()
					}
					if r.Failure == "" && (r.ConsoleHosts != 0 || r.OtherMembers != 0 || r.InventoryFailures != 0 || r.MemberInspectionFailures != 0) {
						r.Failure = "literal_owned_inventory_not_root_only"
					}
					results = append(results, r)
				}
			}
		}
	}
	passed := len(results) == 16
	for _, r := range results {
		common := r.Present && r.Natural && !r.Safety && r.ExitCode != nil && *r.ExitCode == 0 && r.OtherMembers == 0 && r.InventoryFailures == 0 && r.MemberInspectionFailures == 0
		if r.LaunchMode == "detached" {
			passed = passed && common && !r.Matched && !r.Sentinel && r.ConsoleHosts == 0 && r.MaxOwned == 1 && r.Failure == "protocol_ended_early"
		} else {
			passed = passed && common && r.Matched && r.Sentinel && r.ConsoleHosts == 1 && r.MaxOwned == 2 && r.Failure == "literal_owned_inventory_not_root_only"
		}
	}
	r := struct {
		Schema  int                       `json:"schema_version"`
		Commit  string                    `json:"commit"`
		Passed  bool                      `json:"matrix_expectations_passed"`
		Results []powershellLiteralResult `json:"results"`
	}{1, commit, passed, results}
	raw, e := json.MarshalIndent(r, "", "  ")
	if e != nil || writeReceipt(out, append(raw, '\n')) != nil {
		t.Fatal("literal_receipt_failed")
	}
	if !passed {
		t.Fatal("powershell_matrix_expectation_failed")
	}
}
