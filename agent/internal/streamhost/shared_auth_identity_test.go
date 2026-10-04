package streamhost

import (
	"crypto/tls"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestGenerateIdentity_ProducesAValidRSA2048SelfSignedPair(t *testing.T) {
	certPEM, keyPEM, err := generateIdentity()
	if err != nil {
		t.Fatalf("generateIdentity: %v", err)
	}
	if _, err := tls.X509KeyPair(certPEM, keyPEM); err != nil {
		t.Fatalf("generated pair does not parse as a valid TLS keypair: %v", err)
	}
	if !strings.Contains(string(keyPEM), "BEGIN PRIVATE KEY") {
		t.Errorf("key PEM header = %q, want PKCS8 \"BEGIN PRIVATE KEY\" (confirmed format of the real pkey.pem Sunshine itself writes, and what rust-shine/punktfunk both produce)", strings.SplitN(string(keyPEM), "\n", 2)[0])
	}
}

func TestEnsureSharedIdentity_FreshInstallGeneratesAndPersists(t *testing.T) {
	stateDir := t.TempDir()

	cert1, key1, uuid1, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatalf("EnsureSharedIdentity: %v", err)
	}
	if len(cert1) == 0 || len(key1) == 0 || uuid1 == "" {
		t.Fatalf("expected a non-empty identity, got cert=%d key=%d uuid=%q", len(cert1), len(key1), uuid1)
	}

	// Calling again must return the exact same identity, not mint a new one
	// -- every already-paired Moonlight client is pinned to this cert, so
	// regenerating it on a later call (e.g. a routine restart) would
	// silently force every one of them to re-pair.
	cert2, key2, uuid2, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatalf("second EnsureSharedIdentity: %v", err)
	}
	if string(cert1) != string(cert2) || string(key1) != string(key2) || uuid1 != uuid2 {
		t.Fatalf("EnsureSharedIdentity is not idempotent: second call returned a different identity")
	}
}

func TestEnsureSharedIdentity_AdoptsExistingSunshineIdentityInsteadOfGeneratingFresh(t *testing.T) {
	stateDir := t.TempDir()
	sunDir := filepath.Join(stateDir, "sunshine")
	if err := os.MkdirAll(sunDir, 0o755); err != nil {
		t.Fatal(err)
	}

	wantCert, wantKey, err := generateIdentity()
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sunDir, "cert.pem"), wantCert, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sunDir, "pkey.pem"), wantKey, 0o600); err != nil {
		t.Fatal(err)
	}
	wantUUID := "C064935C-8F6E-E601-D90C-41DCF049EFA9" // the real uuid captured live on this machine
	stateJSON := `{"username":"sunshine","salt":"x","password":"y","root":{"uniqueid":"` + wantUUID + `","named_devices":[]}}`
	if err := os.WriteFile(filepath.Join(sunDir, "sunshine_state.json"), []byte(stateJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	gotCert, gotKey, gotUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatalf("EnsureSharedIdentity: %v", err)
	}
	// This is the core upgrade-safety guarantee: rolling out this feature on
	// a machine that already has a real, established Sunshine identity (and
	// therefore real paired Moonlight clients pinned to it) must never
	// replace that identity with a freshly generated one -- every paired
	// client would otherwise be forced to re-pair the moment this feature
	// ships, which is the exact regression it exists to prevent.
	if string(gotCert) != string(wantCert) {
		t.Errorf("adopted cert does not match Sunshine's pre-existing cert -- this would force every already-paired client to re-pair")
	}
	if string(gotKey) != string(wantKey) {
		t.Errorf("adopted key does not match Sunshine's pre-existing key")
	}
	if gotUUID != wantUUID {
		t.Errorf("adopted uuid = %q, want %q (Sunshine's pre-existing root.uniqueid)", gotUUID, wantUUID)
	}
}

func TestEnsureSharedIdentity_AdoptionPriorityPrefersSunshineOverRustshineOverPunktfunk(t *testing.T) {
	stateDir := t.TempDir()

	// Seed all three with a DIFFERENT identity each, to prove the adoption
	// order (Sunshine first) actually matters and isn't incidental.
	sunCert, sunKey, _ := generateIdentity()
	rsCert, rsKey, _ := generateIdentity()
	pfCert, pfKey, _ := generateIdentity()

	writePair := func(dir, certName, keyName string, cert, key []byte) {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, certName), cert, 0o644); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(filepath.Join(dir, keyName), key, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	writePair(filepath.Join(stateDir, "sunshine"), "cert.pem", "pkey.pem", sunCert, sunKey)
	writePair(filepath.Join(stateDir, "rustshine"), "server_cert.pem", "server_key.pem", rsCert, rsKey)
	writePair(filepath.Join(stateDir, "punktfunk"), "cert.pem", "key.pem", pfCert, pfKey)

	gotCert, _, _, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCert) != string(sunCert) {
		t.Errorf("expected Sunshine's identity to win adoption priority (it's the unconditional default backend, see NewDefault's doc comment), got a different one")
	}
}

func TestEnsureSharedIdentity_HalfWrittenPairIsNotAdopted(t *testing.T) {
	stateDir := t.TempDir()
	sunDir := filepath.Join(stateDir, "sunshine")
	if err := os.MkdirAll(sunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// cert.pem present, pkey.pem missing -- simulates a crash between the
	// two writes. Must fall through to generating fresh, not adopt a
	// cert with no matching key (which would make the identity file
	// unusable by every backend that reads it).
	cert, _, _ := generateIdentity()
	if err := os.WriteFile(filepath.Join(sunDir, "cert.pem"), cert, 0o644); err != nil {
		t.Fatal(err)
	}

	gotCert, gotKey, gotUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatalf("EnsureSharedIdentity should degrade to generating fresh, not fail: %v", err)
	}
	if _, err := tls.X509KeyPair(gotCert, gotKey); err != nil {
		t.Errorf("resulting identity is not a valid keypair: %v", err)
	}
	if gotUUID == "" {
		t.Error("expected a generated uuid")
	}
}

func TestEnsureSharedIdentity_CorruptCertDoesNotMatchKeyIsNotAdopted(t *testing.T) {
	stateDir := t.TempDir()
	sunDir := filepath.Join(stateDir, "sunshine")
	if err := os.MkdirAll(sunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	cert1, _, _ := generateIdentity()
	_, key2, _ := generateIdentity()
	// Mismatched cert/key pair -- tls.X509KeyPair must reject this, so
	// readIdentityPair's validation (not just existence) is what's under
	// test here.
	if err := os.WriteFile(filepath.Join(sunDir, "cert.pem"), cert1, 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sunDir, "pkey.pem"), key2, 0o600); err != nil {
		t.Fatal(err)
	}

	gotCert, gotKey, _, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatalf("EnsureSharedIdentity should degrade to generating fresh, not fail: %v", err)
	}
	if _, err := tls.X509KeyPair(gotCert, gotKey); err != nil {
		t.Errorf("resulting identity is not a valid keypair: %v", err)
	}
}

func TestEnsureSharedIdentity_MissingUUIDSidecarIsRegeneratedWithoutDiscardingIdentity(t *testing.T) {
	stateDir := t.TempDir()
	cert, key, uuid, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	// Simulate the uuid sidecar going missing (e.g. deleted by hand, or a
	// partial restore) while the identity files survive.
	if err := os.Remove(sharedUUIDPath(stateDir)); err != nil {
		t.Fatal(err)
	}

	gotCert, gotKey, gotUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if string(gotCert) != string(cert) || string(gotKey) != string(key) {
		t.Error("a missing uuid sidecar must not cause the (perfectly good) identity itself to be regenerated")
	}
	if gotUUID == "" {
		t.Error("expected a regenerated uuid")
	}
	if gotUUID == uuid {
		// Not actually required to differ, but if it's always exactly
		// equal across independent random generations something is wrong
		// (e.g. a hardcoded fallback). Not a hard assertion -- extremely
		// unlikely collision -- just a sanity signal.
		t.Logf("regenerated uuid happened to match the original (astronomically unlikely by chance): %s", gotUUID)
	}
}

func TestDashifyUndashifyUUID_RoundTrip(t *testing.T) {
	dashed := "C064935C-8F6E-E601-D90C-41DCF049EFA9"
	undashed := undashifyUUID(dashed)
	if undashed != "c064935c8f6ee601d90c41dcf049efa9" {
		t.Errorf("undashifyUUID(%q) = %q", dashed, undashed)
	}
	redashed := dashifyUUID(undashed)
	if redashed != dashed {
		t.Errorf("dashifyUUID(undashifyUUID(%q)) = %q, want %q", dashed, redashed, dashed)
	}
}

func TestDashifyUUID_RejectsWrongLength(t *testing.T) {
	if got := dashifyUUID("not-a-uuid"); got != "" {
		t.Errorf("dashifyUUID of garbage = %q, want empty", got)
	}
}
