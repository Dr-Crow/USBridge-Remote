//go:build windows && amd64 && signatureownernative

// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

// These are extraction-native prerequisites, deliberately separate from portable
// tests/cross-compilation and from the original c451d77 native receipt. They have
// not been executed by the extraction's Linux authoring environment.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

func nativeSpec(t *testing.T, script string) Spec {
	t.Helper()
	var buffer [32768]uint16
	n, _, _ := kernel32.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 || n >= uintptr(len(buffer)) {
		t.Fatal("GetSystemDirectoryW failed")
	}
	system := syscall.UTF16ToString(buffer[:n])
	work, err := os.MkdirTemp("", "signatureowner-native-")
	if err != nil {
		t.Fatal(err)
	}
	s := Spec{WorkDir: work, ScriptPath: filepath.Join(work, "signature.ps1"), SystemRoot: filepath.Dir(system), PowerShellPath: filepath.Join(system, "WindowsPowerShell", "v1.0", "powershell.exe"), ConsoleHostPath: filepath.Join(system, "conhost.exe"), Timeout: 30 * time.Second}
	hash := sha256.Sum256([]byte(script))
	s.ScriptSHA256 = hex.EncodeToString(hash[:])
	if err := os.WriteFile(s.ScriptPath, []byte(script), 0600); err != nil {
		t.Fatal(err)
	}
	pin := func(path string) string {
		f, err := os.Open(path)
		if err != nil {
			t.Fatal(err)
		}

		h, err := fileSHA(f, 64<<20)
		closeErr := f.Close()
		if err != nil {
			t.Fatal(err)
		}
		if closeErr != nil {
			t.Fatal("pin handle close failed")
		}
		return h
	}
	s.PowerShellSHA256 = pin(s.PowerShellPath)
	s.ConsoleHostSHA256 = pin(s.ConsoleHostPath)
	return s
}
func TestExtractionNativeOriginalSignature(t *testing.T) {
	s := nativeSpec(t, SignatureScript)
	input, err := json.Marshal(map[string]string{"path": filepath.Join(s.SystemRoot, "System32", "kernel32.dll")})
	if err != nil {
		t.Fatal(err)
	}
	r, err := Run(context.Background(), s, input)
	defer nativeRelease(t, s, r)
	if err != nil {
		t.Fatalf("native original query failed: %v; receipt: %+v", err, r)
	}
	if !r.StartupHandshakeVerified || !r.SuspendedRootVerified || r.SuspendedTotal != 1 || r.SuspendedActive != 1 || r.SuspendedMembers != 1 || !r.NaturalCleanup || !r.CleanupJoined || !r.WatchdogJoined || !r.ResourcesReleased || r.SafetyJobClosed || !r.ConsoleHostVerified || r.TotalOwnedProcesses != 2 || r.ExitCode == nil || *r.ExitCode != 0 || r.ConsoleHostExitCode == nil || *r.ConsoleHostExitCode != 0 {
		t.Fatal("incomplete native ownership proof")
	}
	var output struct {
		Schema int    `json:"schema_version"`
		Status int    `json:"status_code"`
		Hash   string `json:"file_sha256"`
	}
	if json.Unmarshal(r.Output, &output) != nil || output.Schema != 1 || output.Status != 0 || !hashPattern.MatchString(output.Hash) {
		t.Fatal("original signature query did not return bounded signature evidence")
	}
	// Result.Output is json:"-": this marker contains only the closed, source-free
	// ownership receipt. CI accepts it only with an overall passing native suite.
	proof, marshalErr := json.Marshal(r)
	if marshalErr != nil {
		t.Fatal("native proof encoding failed")
	}
	t.Logf("SIGNATURE_OWNER_PROOF_JSON=%s", proof)
}
func TestExtractionNativeFailClosed(t *testing.T) {
	prefix := "[Console]::Out.WriteLine('" + StartupMarker + "'); $null=[Console]::In.ReadLine(); "
	cases := []struct {
		name, script string
		timeout      time.Duration
	}{
		{"wrong_marker", "[Console]::Out.WriteLine('wrong'); exit 0", 30 * time.Second},
		{"no_marker", "[Console]::Out.WriteLine('{}'); exit 0", 30 * time.Second},
		{"partial_result", prefix + "[Console]::Out.Write('{}'); exit 0", 30 * time.Second},
		{"extra_result", prefix + "[Console]::Out.WriteLine('{}'); [Console]::Out.WriteLine('{}'); exit 0", 30 * time.Second},
		{"stderr", prefix + "[Console]::Error.WriteLine('fixed-negative'); [Console]::Out.WriteLine('{}'); exit 0", 30 * time.Second},
		{"nonzero", prefix + "[Console]::Out.WriteLine('{}'); exit 9", 30 * time.Second},
		{"timeout", prefix + "[Threading.Thread]::Sleep(60000); exit 0", 1500 * time.Millisecond},
		// A test-owned inert unknown child must fail even if too short-lived to appear
		// in a PID snapshot. Lifetime Job accounting must still record it. This is a
		// negative fixture, never a production script or an allowed descendant.
		{"unknown_child", prefix + "$i=New-Object Diagnostics.ProcessStartInfo; $i.FileName=$env:SystemRoot+'\\System32\\where.exe'; $i.Arguments='powershell.exe'; $i.UseShellExecute=$false; $i.RedirectStandardOutput=$true; $i.RedirectStandardError=$true; $p=New-Object Diagnostics.Process; $p.StartInfo=$i; $null=$p.Start(); $null=$p.StandardOutput.ReadToEnd(); $null=$p.StandardError.ReadToEnd(); $p.WaitForExit(); [Console]::Out.WriteLine('{}'); exit 0", 30 * time.Second},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := nativeSpec(t, c.script)
			s.Timeout = c.timeout
			started := time.Now()
			r, err := Run(context.Background(), s, []byte("{}"))
			defer nativeRelease(t, s, r)
			if err == nil || len(r.Output) != 0 {
				t.Fatal("negative was accepted")
			}
			if !r.CleanupJoined || !r.WatchdogJoined {
				t.Fatalf("negative cleanup did not join: %+v", r)
			}
			if c.name == "timeout" && (!r.SafetyJobClosed || time.Since(started) > 8*time.Second) {
				t.Fatal("timeout did not force bounded cleanup")
			}
		})
	}
}
func TestExtractionNativePinsBeforeLaunch(t *testing.T) {
	for _, name := range []string{"script", "root", "host", "dependency"} {
		t.Run(name, func(t *testing.T) {
			s := nativeSpec(t, SignatureScript)
			switch name {
			case "script":
				s.ScriptSHA256 = strings.Repeat("0", 64)
			case "root":
				s.PowerShellSHA256 = strings.Repeat("0", 64)
			case "host":
				s.ConsoleHostSHA256 = strings.Repeat("0", 64)
			case "dependency":
				path := filepath.Join(s.WorkDir, "data.bin")
				if err := os.WriteFile(path, []byte("source-owned-negative"), 0600); err != nil {
					t.Fatal(err)
				}
				s.Dependencies = []Dependency{{path, strings.Repeat("0", 64)}}
			}
			r, err := Run(context.Background(), s, []byte("{}"))
			defer nativeRelease(t, s, r)
			if err == nil || r.SuspendedRootVerified || r.StartupHandshakeVerified || len(r.Output) != 0 {
				t.Fatal("invalid pin reached launch")
			}
		})
	}
}

// Uncertain ownership/resource release must retain the caller-owned workspace.
// No t.TempDir cleanup can race a process or worker whose join was not proven.
func nativeRelease(t *testing.T, s Spec, r Result) {
	t.Helper()
	if !r.ResourcesReleased || (r.SuspendedRootVerified && (!r.CleanupJoined || !r.WatchdogJoined)) {
		t.Logf("retained uncertain native workspace: %s", s.WorkDir)
		return
	}
	if err := os.RemoveAll(s.WorkDir); err != nil {
		t.Errorf("native workspace cleanup failed")
	}
}
