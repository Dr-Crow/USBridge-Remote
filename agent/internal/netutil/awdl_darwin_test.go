//go:build darwin

package netutil

import (
	"os"
	"os/exec"
	"strings"
	"testing"
)

func TestAWDLSudoersRuleContent(t *testing.T) {
	got := awdlSudoersRuleContent("alice")
	want := "alice ALL=(root) NOPASSWD: /sbin/ifconfig awdl0 down, /sbin/ifconfig awdl0 up\n"
	if got != want {
		t.Fatalf("awdlSudoersRuleContent(%q) = %q, want %q", "alice", got, want)
	}
}

// TestAWDLSudoersRuleContentIsValidSudoers guards against a regression
// that would silently corrupt /etc/sudoers.d: EnsureAWDLSudoRule always
// runs `visudo -cf` on the generated content before installing it, but
// this test catches a syntax break immediately in CI instead of only at
// install time on an actual Mac.
func TestAWDLSudoersRuleContentIsValidSudoers(t *testing.T) {
	if _, err := exec.LookPath("visudo"); err != nil {
		t.Skip("visudo not available in this environment")
	}
	tmp, err := os.CreateTemp(t.TempDir(), "awdl-sudoers-*")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := tmp.WriteString(awdlSudoersRuleContent("alice")); err != nil {
		t.Fatal(err)
	}
	if err := tmp.Close(); err != nil {
		t.Fatal(err)
	}
	out, err := exec.Command("visudo", "-cf", tmp.Name()).CombinedOutput()
	if err != nil {
		t.Fatalf("visudo -cf rejected generated sudoers rule: %v (%s)", err, strings.TrimSpace(string(out)))
	}
}

func TestShellSingleQuote(t *testing.T) {
	cases := map[string]string{
		"/tmp/plain":  `'/tmp/plain'`,
		"":            `''`,
		"it's/a/path": `'it'\''s/a/path'`,
		"a'b'c":       `'a'\''b'\''c'`,
	}
	for in, want := range cases {
		if got := shellSingleQuote(in); got != want {
			t.Errorf("shellSingleQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAppleScriptQuote(t *testing.T) {
	cases := map[string]string{
		"plain":        `"plain"`,
		`has "quotes"`: `"has \"quotes\""`,
		`back\slash`:   `"back\\slash"`,
	}
	for in, want := range cases {
		if got := appleScriptQuote(in); got != want {
			t.Errorf("appleScriptQuote(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestAWDLSudoersPreviewIncludesPath(t *testing.T) {
	preview := AWDLSudoersPreview()
	if preview == "" {
		t.Fatal("AWDLSudoersPreview() returned empty string")
	}
	if !strings.Contains(preview, sudoersPath) {
		t.Errorf("AWDLSudoersPreview() = %q, want it to mention %q", preview, sudoersPath)
	}
	if !strings.Contains(preview, "awdl0 down") || !strings.Contains(preview, "awdl0 up") {
		t.Errorf("AWDLSudoersPreview() = %q, want it to mention both awdl0 down and up", preview)
	}
}
