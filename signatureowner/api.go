// SPDX-License-Identifier: GPL-3.0-only
// Package signatureowner owns one immutable Windows signature-query invocation.
// It does not decide publisher trust, admit DLLs, execute acquired images, or
// provide a general command runner. See PROVENANCE.md for the extraction boundary.
package signatureowner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

const (
	MaxInputBytes      = 4096
	MaxOutputBytes     = 4096
	MaxScriptBytes     = 65536
	MaxDependencies    = 8
	MaxDependencyBytes = 64 << 20
	DefaultTimeout     = 30 * time.Second
	MaxTimeout         = 30 * time.Minute
	maxProtocolLine    = MaxOutputBytes
)

var ErrUnsupported = errors.New("signature_owner_requires_windows_amd64")
var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)

// Spec pins the complete executable and immutable-script identities. All paths
// must be absolute local-drive Windows paths. ScriptPath must be a direct child
// of the caller's existing private WorkDir. The package creates no files.
// SystemRoot and both executable paths must match Windows' installed directories.
// ScriptSHA256 and every dependency pin MUST be immutable source-reviewed
// constants chosen by the trusted caller (dependency pins may come from a
// validated source-build receipt), never values accepted from an acquired
// image, descriptor, remote request or command-line input. Each reviewed script
// must emit StartupMarker, wait for one private stdin line, use only in-process
// managed APIs/cmdlets (no Add-Type/compiler or process launch), emit one JSON
// object and exit. SignatureScript is the original reviewed default example.
// SHA256s are continuity pins supplied by the trusted caller, not trust evidence.
// Timeout bounds the entire invocation, including preparation/startup/query/EOF,
// and defaults to DefaultTimeout. It must not exceed MaxTimeout. Context
// cancellation can only shorten it. Failure cleanup has a separate 3s join bound.
// No args, environment, runtime script body, child allowlist or launch seam is public.
type Spec struct {
	WorkDir           string
	ScriptPath        string
	ScriptSHA256      string
	SystemRoot        string
	PowerShellPath    string
	PowerShellSHA256  string
	ConsoleHostPath   string
	ConsoleHostSHA256 string
	Dependencies      []Dependency
	Timeout           time.Duration
}

// Dependency is a caller-source-pinned assembly or data file needed by the
// reviewed script. It must be a direct child of WorkDir. Each file is held with
// Windows write/delete sharing denied through cleanup. Limits are deliberately
// small; queried images are inputs, not executable dependencies.
type Dependency struct {
	Path   string
	SHA256 string
}

// Result is a bounded process-ownership receipt. Output contains one JSON object
// only after successful natural retirement and all worker/watchdog joins. A nil
// error proves the owner/protocol contract, not successful signature verification:
// the fixed script can return a failure_stage object, which callers must handle.
// ResourcesReleased is true only after all acquired resource closes succeed.
// A ResourceCloseError preserves any primary error and removes Output.
// Result contains no process IDs, absolute paths, stdin, stderr or raw OS errors.
type Result struct {
	ResourcesReleased        bool     `json:"resources_released"`
	ResourceCloseFailures    []string `json:"resource_close_failures,omitempty"`
	Output                   []byte   `json:"-"`
	StartupHandshakeVerified bool     `json:"startup_handshake_verified"`
	SuspendedTotal           uint32   `json:"suspended_total_processes"`
	SuspendedActive          uint32   `json:"suspended_active_processes"`
	SuspendedMembers         uint32   `json:"suspended_members"`
	SuspendedRootVerified    bool     `json:"suspended_root_verified"`
	VerifierSHA              string   `json:"verifier_sha256,omitempty"`
	ScriptSHA                string   `json:"verifier_script_sha256,omitempty"`
	ConsoleHostSHA           string   `json:"console_host_sha256,omitempty"`
	ConsoleHostVerified      bool     `json:"console_host_identity_verified"`
	TotalOwnedProcesses      uint32   `json:"total_owned_processes"`
	NaturalCleanup           bool     `json:"natural_cleanup"`
	CleanupJoined            bool     `json:"cleanup_joined"`
	WatchdogJoined           bool     `json:"watchdog_joined"`
	SafetyJobClosed          bool     `json:"safety_job_closed"`
	ExitCode                 *uint32  `json:"root_exit_code,omitempty"`
	ConsoleHostExitCode      *uint32  `json:"console_host_exit_code,omitempty"`
	ElapsedMillis            int64    `json:"elapsed_ms"`
	CleanupFailure           string   `json:"cleanup_failure,omitempty"`
}

// Run sends exactly one bounded JSON object after the literal readiness marker
// and exact two-process startup proof. Input is copied and never placed on argv,
// in the environment, or in a file. It may have one trailing LF or CRLF. The
// source-reviewed script defines its own request/result schema; the caller must
// validate those schemas and authorization. This API supplies no shell arguments.
func Run(ctx context.Context, spec Spec, input []byte) (r Result, err error) {
	started := time.Now()
	defer func() { r.ElapsedMillis = time.Since(started).Milliseconds() }()
	if ctx == nil {
		return r, failure("signature_context_missing")
	}
	if err = validateSpec(spec); err != nil {
		return r, err
	}
	spec.Dependencies = append([]Dependency(nil), spec.Dependencies...)
	request, err := prepareInput(input)
	if err != nil {
		return r, err
	}
	defer clear(request)
	duration := spec.Timeout
	if duration == 0 {
		duration = DefaultTimeout
	}
	ctx, cancel := context.WithDeadline(ctx, started.Add(duration))
	defer cancel()
	if err = ctx.Err(); err != nil {
		return r, err
	}
	return run(ctx, spec, request)
}

func validateSpec(s Spec) error {
	if s.Timeout < 0 || s.Timeout > MaxTimeout {
		return failure("signature_budget_invalid")
	}
	for _, path := range []string{s.WorkDir, s.ScriptPath, s.SystemRoot, s.PowerShellPath, s.ConsoleHostPath} {
		if !localWindowsPath(path) {
			return failure("signature_path_invalid")
		}
	}
	work, script, root := normalizedWindowsPath(s.WorkDir), normalizedWindowsPath(s.ScriptPath), normalizedWindowsPath(s.SystemRoot)
	slash := strings.LastIndexByte(script, '\\')
	if slash < 0 || !strings.EqualFold(script[:slash], work) || !strings.HasSuffix(strings.ToLower(script), ".ps1") {
		return failure("signature_script_location_invalid")
	}
	if !strings.EqualFold(normalizedWindowsPath(s.PowerShellPath), root+`\System32\WindowsPowerShell\v1.0\powershell.exe`) || !strings.EqualFold(normalizedWindowsPath(s.ConsoleHostPath), root+`\System32\conhost.exe`) {
		return failure("signature_system_binary_path_invalid")
	}
	if !hashPattern.MatchString(s.ScriptSHA256) || !hashPattern.MatchString(s.PowerShellSHA256) || !hashPattern.MatchString(s.ConsoleHostSHA256) {
		return failure("signature_pin_invalid")
	}
	if len(s.Dependencies) > MaxDependencies {
		return failure("signature_dependency_bounds")
	}
	seen := map[string]bool{strings.ToLower(script): true}
	for _, dependency := range s.Dependencies {
		path := normalizedWindowsPath(dependency.Path)
		slash := strings.LastIndexByte(path, '\\')
		if !localWindowsPath(path) || slash < 0 || !strings.EqualFold(path[:slash], work) || seen[strings.ToLower(path)] || !hashPattern.MatchString(dependency.SHA256) {
			return failure("signature_dependency_invalid")
		}
		seen[strings.ToLower(path)] = true
	}
	return nil
}

func normalizedWindowsPath(s string) string { return strings.ReplaceAll(s, "/", `\`) }
func localWindowsPath(s string) bool {
	s = normalizedWindowsPath(s)
	if len(s) < 4 || len(s) > 32760 || !((s[0] >= 'A' && s[0] <= 'Z') || (s[0] >= 'a' && s[0] <= 'z')) || s[1:3] != `:\` {
		return false
	}
	for _, c := range s[3:] {
		if c < 32 || c == 127 || strings.ContainsRune(`:"<>|?*`, c) {
			return false
		}
	}
	for _, part := range strings.Split(s[3:], `\`) {
		if part == "" || part == "." || part == ".." || strings.HasSuffix(part, ".") || strings.HasSuffix(part, " ") {
			return false
		}
		base := strings.ToUpper(strings.SplitN(part, ".", 2)[0])
		switch base {
		case "CON", "PRN", "AUX", "NUL", "CONIN$", "CONOUT$":
			return false
		}
		if len(base) == 4 && (strings.HasPrefix(base, "COM") || strings.HasPrefix(base, "LPT")) && base[3] >= '1' && base[3] <= '9' {
			return false
		}
	}
	return true
}

func prepareInput(input []byte) ([]byte, error) {
	if len(input) == 0 || len(input) > MaxInputBytes {
		return nil, failure("signature_input_bounds")
	}
	raw := input
	if bytes.HasSuffix(raw, []byte("\n")) {
		raw = bytes.TrimSuffix(raw, []byte("\n"))
		raw = bytes.TrimSuffix(raw, []byte("\r"))
	}
	if bytes.ContainsAny(raw, "\r\n") {
		return nil, failure("signature_input_lines")
	}
	if err := oneJSONObject(raw); err != nil {
		return nil, failure("signature_input_invalid")
	}
	if len(raw)+1 > MaxInputBytes {
		return nil, failure("signature_input_bounds")
	}
	return append(append([]byte(nil), raw...), '\n'), nil
}

func validateOutput(raw []byte) error {
	if len(raw) == 0 || len(raw) > MaxOutputBytes || raw[len(raw)-1] != '\n' {
		return failure("signature_output_bounds")
	}
	line := bytes.TrimSuffix(bytes.TrimSuffix(raw, []byte("\n")), []byte("\r"))
	if bytes.ContainsAny(line, "\r\n") || oneJSONObject(line) != nil {
		return failure("signature_output_invalid")
	}
	return nil
}

// Reject duplicate/case-aliased keys, non-object roots, trailing data and excessive nesting.
// Actual query schemas remain the caller's responsibility.
func oneJSONObject(raw []byte) error {
	if !utf8.Valid(raw) {
		return fmt.Errorf("json_utf8")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.UseNumber()
	first, err := d.Token()
	if err != nil || first != json.Delim('{') {
		return fmt.Errorf("object_required")
	}
	var value func(json.Token, int) error
	value = func(token json.Token, depth int) error {
		if depth > 32 {
			return fmt.Errorf("json_depth")
		}
		delimiter, compound := token.(json.Delim)
		if !compound {
			return nil
		}
		switch delimiter {
		case '{':
			seen := map[string]bool{}
			for d.More() {
				k, e := d.Token()
				if e != nil {
					return e
				}
				key, ok := k.(string)
				if !ok || seen[strings.ToLower(key)] {
					return fmt.Errorf("duplicate_key")
				}
				seen[strings.ToLower(key)] = true
				v, e := d.Token()
				if e != nil {
					return e
				}
				if e = value(v, depth+1); e != nil {
					return e
				}
			}
			t, e := d.Token()
			if e != nil || t != json.Delim('}') {
				return fmt.Errorf("object_end")
			}
		case '[':
			for d.More() {
				v, e := d.Token()
				if e != nil {
					return e
				}
				if e = value(v, depth+1); e != nil {
					return e
				}
			}
			t, e := d.Token()
			if e != nil || t != json.Delim(']') {
				return fmt.Errorf("array_end")
			}
		default:
			return fmt.Errorf("unexpected_delimiter")
		}
		return nil
	}
	if err = value(first, 1); err != nil {
		return err
	}
	if _, err = d.Token(); err != io.EOF {
		return fmt.Errorf("trailing_data")
	}
	return nil
}

func failure(code string) error { return errors.New(code) }
func naturalChildExit(exitCode uint32, safetyClosed bool) error {
	if safetyClosed {
		return failure("safety_job_closed")
	}
	if exitCode != 0 {
		return failure("child_nonzero_exit")
	}
	return nil
}
func fileSHA(f *os.File, limit int64) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", failure("file_read_failed")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, limit+1))
	if err != nil || n == 0 || n > limit {
		return "", failure("file_bounds_failed")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
