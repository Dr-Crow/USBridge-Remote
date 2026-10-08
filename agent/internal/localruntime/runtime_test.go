package localruntime

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/base64"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestDisabledByDefault(t *testing.T) {
	t.Setenv(Environment, "")
	if Enabled() {
		t.Fatal("enabled by default")
	}
	if _, err := Prepare("missing", t.TempDir(), "rustshine", "test"); err == nil {
		t.Fatal("disabled mode accepted")
	}
}
func TestRekeyRequiresExactlyOneAuditedConstant(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	for _, b := range [][]byte{[]byte("no key"), []byte(vendorKey + vendorKey)} {
		if _, err := rekey(b, pub); err == nil {
			t.Fatal("ambiguous input accepted")
		}
	}
}
func TestRekeyDoesNotMutateSource(t *testing.T) {
	pub, _, _ := ed25519.GenerateKey(rand.Reader)
	src := []byte("before" + vendorKey + "after")
	copyBefore := append([]byte{}, src...)
	out, err := rekey(src, pub)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(src, copyBefore) {
		t.Fatal("original modified")
	}
	if len(out) != len(src) || bytes.Contains(out, []byte(vendorKey)) || !bytes.Contains(out, []byte(base64.StdEncoding.EncodeToString(pub))) {
		t.Fatal("bad constant replacement")
	}
}
func TestLocalTokenHasSeparateTrustAndBoundedExpiry(t *testing.T) {
	pub, priv, _ := ed25519.GenerateKey(rand.Reader)
	s := &session{dir: t.TempDir(), hw: "local-test-hardware", private: priv, public: pub}
	now := time.Unix(1900000000, 0)
	if err := s.writeToken(now); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(s.dir, "entitlement.token"))
	if err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(string(data), ".")
	if len(parts) != 3 || parts[0] != "usbent1" {
		t.Fatal("bad token")
	}
	sig, _ := base64.RawURLEncoding.DecodeString(parts[2])
	if !ed25519.Verify(pub, []byte(parts[1]), sig) {
		t.Fatal("local signature invalid")
	}
	vendor, _ := base64.StdEncoding.DecodeString(vendorKey)
	if ed25519.Verify(vendor, []byte(parts[1]), sig) {
		t.Fatal("local token accepted by vendor key")
	}
	raw, _ := base64.RawURLEncoding.DecodeString(parts[1])
	var c struct {
		Provider, Sub, Tier string
		Exp                 int64
	}
	if err = json.Unmarshal(raw, &c); err != nil {
		t.Fatal(err)
	}
	if c.Provider != "desktop-device" || c.Sub != s.hw || c.Tier != "pro" || c.Exp != now.Add(24*time.Hour).Unix() {
		t.Fatalf("wrong local claims: %+v", c)
	}
	if err = s.writeToken(now.Add(time.Hour)); err != nil {
		t.Fatalf("atomic renewal: %v", err)
	}
}
func TestPrepareRefusesUnknownExecutable(t *testing.T) {
	t.Setenv(Environment, "1")
	dir := t.TempDir()
	p := filepath.Join(dir, "unknown")
	os.WriteFile(p, []byte(vendorKey), 0600)
	if _, err := Prepare(p, dir, "rustshine", "test"); err == nil {
		t.Fatal("unknown executable accepted")
	}
	if _, err := os.Stat(filepath.Join(dir, "local-runtime")); !os.IsNotExist(err) {
		t.Fatal("created session before verifying executable")
	}
}

func TestConfiguredStartupAndExplicitOverride(t *testing.T) {
	original := configuredMode.Load()
	t.Cleanup(func() { configuredMode.Store(original) })
	for _, tc := range []struct {
		env              string
		configured, want bool
	}{
		{"", false, false}, {"", true, true}, {"1", false, true}, {"1", true, true},
	} {
		t.Setenv(Environment, tc.env)
		if err := Configure(tc.configured); err != nil {
			t.Fatal(err)
		}
		if Enabled() != tc.want {
			t.Fatalf("env=%q configured=%t: enabled=%t", tc.env, tc.configured, Enabled())
		}
	}
}

func TestPreparedDoesNotFollowEnabledOrUnknownBinary(t *testing.T) {
	t.Setenv(Environment, "1")
	dir := t.TempDir()
	if Prepared(dir, "rustshine") || Prepared(dir, "usb-broker") {
		t.Fatal("mode enabled fabricated preparation")
	}
	p := filepath.Join(dir, "unknown")
	if err := os.WriteFile(p, []byte("not a pinned binary"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Prepare(p, dir, "rustshine", "test"); err == nil {
		t.Fatal("unknown binary accepted")
	}
	if Prepared(dir, "rustshine") {
		t.Fatal("failed preparation marked patched")
	}
}

func TestSavedModeDoesNotLeakIntoRelaunchEnvironment(t *testing.T) {
	t.Setenv(Environment, "")
	original := configuredMode.Load()
	t.Cleanup(func() { configuredMode.Store(original) })
	if err := Configure(true); err != nil {
		t.Fatal(err)
	}
	if !Enabled() || os.Getenv(Environment) != "" {
		t.Fatal("saved mode leaked into child environment")
	}
	if err := Configure(false); err != nil {
		t.Fatal(err)
	}
	if Enabled() {
		t.Fatal("fresh engine retained stale saved mode")
	}
}
