// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"os"
	"reflect"
	"strings"
	"testing"
	"time"
)

func validSpec() Spec {
	return Spec{WorkDir: `C:\private\query`, ScriptPath: `C:\private\query\signature.ps1`, ScriptSHA256: SignatureScriptSHA256, SystemRoot: `C:\Windows`, PowerShellPath: `C:\Windows\System32\WindowsPowerShell\v1.0\powershell.exe`, PowerShellSHA256: strings.Repeat("a", 64), ConsoleHostPath: `C:\Windows\System32\conhost.exe`, ConsoleHostSHA256: strings.Repeat("b", 64)}
}
func TestSpecRejectsUnboundedAndAmbiguousLaunch(t *testing.T) {
	for name, modify := range map[string]func(*Spec){
		"relative_work":       func(s *Spec) { s.WorkDir = "private" },
		"unc":                 func(s *Spec) { s.WorkDir = `\\host\share` },
		"device":              func(s *Spec) { s.WorkDir = `\\?\C:\private` },
		"traversal":           func(s *Spec) { s.ScriptPath = `C:\private\query\..\evil.ps1` },
		"outside":             func(s *Spec) { s.ScriptPath = `C:\other\signature.ps1` },
		"alternate_stream":    func(s *Spec) { s.ScriptPath += ":stream" },
		"wrong_ext":           func(s *Spec) { s.ScriptPath = `C:\private\query\script.cmd` },
		"nul":                 func(s *Spec) { s.WorkDir += "\x00" },
		"root_alias":          func(s *Spec) { s.SystemRoot = `C:\Windows.` },
		"wrong_root":          func(s *Spec) { s.PowerShellPath = `C:\other\powershell.exe` },
		"pwsh":                func(s *Spec) { s.PowerShellPath = `C:\Windows\System32\pwsh.exe` },
		"syshost":             func(s *Spec) { s.ConsoleHostPath = `C:\Windows\SysWOW64\conhost.exe` },
		"empty_script_pin":    func(s *Spec) { s.ScriptSHA256 = "" },
		"uppercase_pin":       func(s *Spec) { s.PowerShellSHA256 = strings.Repeat("A", 64) },
		"negative_budget":     func(s *Spec) { s.Timeout = -1 },
		"excessive_budget":    func(s *Spec) { s.Timeout = MaxTimeout + 1 },
		"external_dependency": func(s *Spec) { s.Dependencies = []Dependency{{`C:\other\Verifier.dll`, strings.Repeat("c", 64)}} },
		"duplicate_dependency": func(s *Spec) {
			s.Dependencies = []Dependency{{`C:\private\query\signature.ps1`, SignatureScriptSHA256}}
		},
		"case_alias_dependency": func(s *Spec) {
			s.Dependencies = []Dependency{{`C:\PRIVATE\QUERY\SIGNATURE.PS1`, SignatureScriptSHA256}}
		},
		"many_dependencies": func(s *Spec) { s.Dependencies = make([]Dependency, MaxDependencies+1) },
	} {
		t.Run(name, func(t *testing.T) {
			s := validSpec()
			modify(&s)
			if validateSpec(s) == nil {
				t.Fatal("invalid launch accepted")
			}
		})
	}
	for _, budget := range []time.Duration{0, 900 * time.Second, MaxTimeout} {
		s := validSpec()
		s.Timeout = budget
		s.Dependencies = []Dependency{{`C:\private\query\Verifier.dll`, strings.Repeat("c", 64)}}
		if e := validateSpec(s); e != nil {
			t.Fatal(e)
		}
	}
}
func TestAbsoluteLocalWindowsPaths(t *testing.T) {
	for _, s := range []string{"", `C:relative`, `C:\`, `/tmp/file`, `\\host\x`, `C:\x\..\y`, `C:\x\y.`, `C:\x\y `, `C:\x\NUL.txt`, `C:\x\COM1.dll`, `C:\x\a:b`, `C:\x\\y`, `C:\x\*.dll`, `C:\x\a|b`} {
		if localWindowsPath(s) {
			t.Fatalf("accepted %q", s)
		}
	}
	for _, s := range []string{`C:\Windows\kernel32.dll`, `d:/private/query/file.ps1`, `C:\some space\normal.dll`} {
		if !localWindowsPath(s) {
			t.Fatalf("rejected %q", s)
		}
	}
}
func TestScriptAndArgumentsArePinned(t *testing.T) {
	h := sha256.Sum256([]byte(SignatureScript))
	if hex.EncodeToString(h[:]) != SignatureScriptSHA256 {
		t.Fatal("original script pin changed")
	}
	want := []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-File", `C:\private\query\script.ps1`}
	if !reflect.DeepEqual(signatureArguments(want[4]), want) {
		t.Fatal("shell arguments widened")
	}
	for _, bad := range []string{"Add-Type", "Start-Process", "-ExecutionPolicy", "Invoke-Expression", "powershell.exe"} {
		if strings.Contains(SignatureScript, bad) {
			t.Fatalf("unexpected script capability: %s", bad)
		}
	}
}
func TestRequestIsCopiedAndBoundedOneObject(t *testing.T) {
	original := []byte("{\"path\":\"C:\\\\private\\\\file.iso\"}\r\n")
	save := append([]byte(nil), original...)
	got, err := prepareInput(original)
	if err != nil {
		t.Fatal(err)
	}
	clear(got)
	if !bytes.Equal(original, save) {
		t.Fatal("caller input mutated")
	}
	for _, bad := range []string{"", "null", "[]", "123", "{} {}", "{}\r", "{}\n{}", "{\n}", "{\"path\":1,\"path\":2}", "{\"path\":1,\"PATH\":2}", "{\"nested\":{\"x\":1,\"x\":2}}", strings.Repeat(" ", MaxInputBytes+1), "{\"x\":" + strings.Repeat("[", 40) + "0" + strings.Repeat("]", 40) + "}"} {
		if _, err := prepareInput([]byte(bad)); err == nil {
			t.Fatalf("bad request accepted: %.80q", bad)
		}
	}
	for _, good := range []string{"{}", "{\"nested\":[1,true,null,{\"x\":1}]}\n"} {
		if _, err := prepareInput([]byte(good)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestOutputRejectsPartialExtraAndMalformedData(t *testing.T) {
	for _, bad := range []string{"", "{}", "{}\n{}\n", "null\n", "[]\n", "{\"x\":1,\"x\":2}\n", strings.Repeat(" ", MaxOutputBytes) + "{}\n", "{\n}\n", "{\"x\":NaN}\n"} {
		if validateOutput([]byte(bad)) == nil {
			t.Fatalf("bad output accepted: %.80q", bad)
		}
	}
	for _, good := range []string{"{}\n", "{\"failure_stage\":\"read_request\"}\r\n"} {
		if err := validateOutput([]byte(good)); err != nil {
			t.Fatal(err)
		}
	}
}
func TestCancelledContextPreventsInvocation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r, err := Run(ctx, validSpec(), []byte("{}"))
	if !errors.Is(err, context.Canceled) || len(r.Output) != 0 || r.StartupHandshakeVerified {
		t.Fatal("cancelled run progressed")
	}
	if _, err := Run(nil, validSpec(), []byte("{}")); err == nil {
		t.Fatal("nil context accepted")
	}
}
func TestForcedZeroExitNeverNatural(t *testing.T) {
	if naturalChildExit(0, true) == nil || naturalChildExit(1, false) == nil || naturalChildExit(0, false) != nil {
		t.Fatal("forced cleanup became natural")
	}
}
func TestBoundedFileHash(t *testing.T) {
	f, err := os.CreateTemp(t.TempDir(), "hash")
	if err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if _, err = fileSHA(f, 1); err == nil {
		t.Fatal("empty file accepted")
	}
	if _, err = f.WriteString("ab"); err != nil {
		t.Fatal(err)
	}
	if _, err = fileSHA(f, 1); err == nil {
		t.Fatal("oversized file accepted")
	}
	h := sha256.Sum256([]byte("ab"))
	if got, err := fileSHA(f, 2); err != nil || got != hex.EncodeToString(h[:]) {
		t.Fatal("wrong hash")
	}
}

func TestProtocolCapsRejectRatherThanTruncate(t *testing.T) {
	// Protocol carries counters/hashes; a bounded aggregate inventory is an
	// independently hash-pinned dependency file, not silently truncated stdin.
	accepted := []byte(`{"x":"` + strings.Repeat("a", MaxInputBytes-9) + `"}`)
	got, err := prepareInput(accepted)
	if err != nil || len(got) != MaxInputBytes || !bytes.Equal(got[:len(got)-1], accepted) {
		t.Fatal("boundary input altered or rejected")
	}
	if _, err = prepareInput(append(accepted, ' ')); err == nil {
		t.Fatal("oversized input silently truncated")
	}
	if validateOutput(got) != nil {
		t.Fatal("boundary result rejected")
	}
	if validateOutput(append([]byte(" "), got...)) == nil {
		t.Fatal("oversized result silently truncated")
	}
}
