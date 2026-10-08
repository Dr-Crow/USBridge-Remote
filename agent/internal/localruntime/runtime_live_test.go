package localruntime

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/json"
	"net"
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
	if !Prepared(dir, "usb-broker") || Prepared(dir, "rustshine") {
		t.Fatal("incorrect preparation status")
	}
	copied, err := os.ReadFile(spec.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(spec.Binary, []byte("tampered copy"), 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(source, dir, "usb-broker", "local-ci-test"); err == nil {
		t.Fatal("tampered copy accepted")
	}
	if Prepared(dir, "usb-broker") {
		t.Fatal("tampered copy still marked prepared")
	}
	if err := os.WriteFile(spec.Binary, copied, 0700); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(source, dir, "usb-broker", "local-ci-test"); err != nil {
		t.Fatal(err)
	}
	reserve, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	control := reserve.Addr().String()
	reserve.Close()
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, spec.Binary, "--role", "agent", "--listen", "127.0.0.1:0", "--control", control, "--secret", strings.Repeat("0", 64), "--hardware-id", "local-ci-test", "--entitlement-file", spec.Token)
	cmd.Dir = dir
	var output bytes.Buffer
	cmd.Stdout = &output
	cmd.Stderr = &output
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	waited := false
	defer func() {
		if !waited {
			cancel()
			_ = cmd.Wait()
		}
	}()
	var conn net.Conn
	for deadline := time.Now().Add(5 * time.Second); time.Now().Before(deadline); time.Sleep(50 * time.Millisecond) {
		conn, err = net.DialTimeout("tcp4", control, 200*time.Millisecond)
		if err == nil {
			break
		}
	}
	if conn == nil {
		cancel()
		_ = cmd.Wait()
		waited = true
		t.Fatalf("no control listener: %s", output.String())
	}
	conn.SetDeadline(time.Now().Add(2 * time.Second))
	_, err = conn.Write([]byte("{\"cmd\":\"hid_stream\"}\n"))
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	line, err := bufio.NewReader(conn).ReadBytes('\n')
	conn.Close()
	if err != nil {
		t.Fatal(err)
	}
	var capability struct {
		OK     bool `json:"ok"`
		RawHID bool `json:"raw_hid"`
		Wacom  bool `json:"wacom"`
	}
	if err := json.Unmarshal(line, &capability); err != nil {
		t.Fatal(err)
	}
	if !capability.OK || !capability.RawHID || !capability.Wacom {
		t.Fatalf("local Pro HID capability not accepted: %s", line)
	}
	t.Log("Broker reports raw_hid=true and wacom=true; no device descriptor or input was sent")
	cancel()
	_ = cmd.Wait()
	waited = true
	if strings.Contains(output.String(), "BadSignature") {
		t.Fatalf("unexpected signature rejection: %s", output.String())
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
	if filepath.Dir(spec.Token) == filepath.Dir(source) {
		t.Fatal("local token mixed with vendor installation")
	}
}
