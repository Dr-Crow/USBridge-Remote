package localruntime

import (
	"context"
	"crypto/sha256"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Explicit opt-in integration test, never run against an arbitrary local app.
// Uses the hash-pinned vendor broker, loopback-only ephemeral ports, no devices.
func TestPinnedBrokerLocalRuntime(t *testing.T) {
	source := os.Getenv("USBRIDGE_LAB_TEST_BINARY")
	if source == "" {
		t.Skip("explicit pinned broker path required")
	}
	t.Setenv(Environment, "1")
	defer Close()
	original, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(original)
	dir := t.TempDir()
	spec, err := Prepare(source, dir, "usb-broker", "local-ci-test")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, spec.Binary, "--role", "agent", "--listen", "127.0.0.1:0", "--control", "127.0.0.1:0", "--secret", strings.Repeat("0", 64), "--hardware-id", "local-ci-test", "--entitlement-file", spec.Token)
	cmd.Dir = dir
	output, runErr := cmd.CombinedOutput()
	if !strings.Contains(string(output), "control listen") || strings.Contains(string(output), "BadSignature") {
		t.Fatalf("broker did not reach local control listener: %v\n%s", runErr, output)
	}
	after, err := os.ReadFile(source)
	if err != nil || sha256.Sum256(after) != before {
		t.Fatal("vendor original changed")
	}
	second, err := Prepare(source, dir, "usb-broker", "local-ci-test")
	if err != nil || second != spec {
		t.Fatalf("reuse failed: %v", err)
	}
	if _, err := Prepare(source, dir, "usb-broker", "different-hardware"); err == nil {
		t.Fatal("identity change accepted")
	}
	if err := os.WriteFile(spec.Binary, []byte("tampered copy"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(source, dir, "usb-broker", "local-ci-test"); err == nil {
		t.Fatal("tampered copy accepted")
	}
	if filepath.Dir(spec.Token) == filepath.Dir(source) {
		t.Fatal("local token mixed with vendor installation")
	}
}
