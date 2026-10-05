package streamhost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestReconcileSharedAuth_PairingViaOneBackendPropagatesToTheOtherTwo is the
// core scenario the user asked for: pair a device while Sunshine is active,
// then simulate switching to rust-shine and to punktfunk (each backend's
// Start() calls ReconcileSharedAuth -- see sunshine_backend.go/
// rustshine_backend.go/punktfunk_backend.go), and confirm the client ends
// up trusted by all three without ever touching an admin API.
func TestReconcileSharedAuth_PairingViaOneBackendPropagatesToTheOtherTwo(t *testing.T) {
	stateDir := t.TempDir()

	// Establish the shared identity first (as sunshineBackend.Start would).
	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}

	// Simulate a real pairing having just happened against Sunshine: its
	// own trust file now has one client that canonical doesn't know about
	// yet.
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{
		{Fingerprint: fp, UniqueID: "client-1", Name: "", CertDER: der, Enabled: "true"},
	}); err != nil {
		t.Fatal(err)
	}

	// Switching to rust-shine calls ReconcileSharedAuth as part of Start().
	ReconcileSharedAuth(stateDir)

	rsClients, err := readRustshineTrusted(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFingerprint(rsClients, fp) {
		t.Fatalf("rust-shine's trusted_clients.pem does not contain the client paired via Sunshine: %+v", rsClients)
	}

	pfClients, err := readPunktfunkTrusted(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFingerprint(pfClients, fp) {
		t.Fatalf("punktfunk's paired.json does not contain the client paired via Sunshine: %+v", pfClients)
	}
}

// TestReconcileSharedAuth_PairingViaPunktfunkPropagatesBackward proves the
// propagation direction isn't hardcoded to "Sunshine is the source of
// truth" -- punktfunk has no uniqueid at all, only a fingerprint, so this
// also exercises the path with the least metadata available.
func TestReconcileSharedAuth_PairingViaPunktfunkPropagatesBackward(t *testing.T) {
	stateDir := t.TempDir()
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	if err := writePunktfunkTrusted(stateDir, []trustedClient{{Fingerprint: fp, CertDER: der}}); err != nil {
		t.Fatal(err)
	}

	ReconcileSharedAuth(stateDir)

	_, sunClients, err := readSunshineTrusted(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFingerprint(sunClients, fp) {
		t.Fatalf("sunshine_state.json does not contain the client paired via punktfunk: %+v", sunClients)
	}
	rsClients, err := readRustshineTrusted(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if !containsFingerprint(rsClients, fp) {
		t.Fatalf("trusted_clients.pem does not contain the client paired via punktfunk: %+v", rsClients)
	}
}

// TestReconcileSharedAuth_IsIdempotentAndConverges proves running
// ReconcileSharedAuth repeatedly (as every single Start() does) never
// duplicates entries and never drifts once everything already agrees --
// the exact "no regressions across future updates/restarts" property the
// feature was asked to be tested for.
func TestReconcileSharedAuth_IsIdempotentAndConverges(t *testing.T) {
	stateDir := t.TempDir()
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{{Fingerprint: fp, UniqueID: "c1", CertDER: der}}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 5; i++ {
		ReconcileSharedAuth(stateDir)
	}

	_, sunClients, _ := readSunshineTrusted(stateDir)
	rsClients, _ := readRustshineTrusted(stateDir)
	pfClients, _ := readPunktfunkTrusted(stateDir)
	for name, clients := range map[string][]trustedClient{"sunshine": sunClients, "rustshine": rsClients, "punktfunk": pfClients} {
		n := countFingerprint(clients, fp)
		if n != 1 {
			t.Errorf("%s has %d entries for the same fingerprint after 5 reconciles, want exactly 1 (duplication bug)", name, n)
		}
	}
}

// TestRemoveTrustedClientEverywhere_RemovesFromAllThreeBackends is the
// other half of the user's explicit request: unpairing must remove the
// client from every backend, not just the one it was unpaired through.
func TestRemoveTrustedClientEverywhere_RemovesFromAllThreeBackends(t *testing.T) {
	stateDir := t.TempDir()
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{{Fingerprint: fp, UniqueID: "c1", CertDER: der}}); err != nil {
		t.Fatal(err)
	}
	ReconcileSharedAuth(stateDir) // propagate to all three first

	if err := RemoveTrustedClientEverywhere(stateDir, "sunshine", "c1"); err != nil {
		t.Fatalf("RemoveTrustedClientEverywhere: %v", err)
	}

	_, sunClients, _ := readSunshineTrusted(stateDir)
	rsClients, _ := readRustshineTrusted(stateDir)
	pfClients, _ := readPunktfunkTrusted(stateDir)
	if containsFingerprint(sunClients, fp) {
		t.Error("still present in sunshine_state.json after removal")
	}
	if containsFingerprint(rsClients, fp) {
		t.Error("still present in trusted_clients.pem after removal")
	}
	if containsFingerprint(pfClients, fp) {
		t.Error("still present in paired.json after removal")
	}
}

// TestRemoveTrustedClientEverywhere_FixesTheReportedSunshineCannotDeleteBug
// is the direct regression test for the bug report: even if Sunshine's own
// file somehow still has the entry afterward (simulating its admin-API
// unpair having silently failed to persist, the reported symptom),
// reconciling again must NEVER resurrect it into the other two backends --
// the tombstone (trustStoreFile.Removed) is what prevents that.
func TestRemoveTrustedClientEverywhere_FixesTheReportedSunshineCannotDeleteBug(t *testing.T) {
	stateDir := t.TempDir()
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{{Fingerprint: fp, UniqueID: "c1", CertDER: der}}); err != nil {
		t.Fatal(err)
	}
	ReconcileSharedAuth(stateDir)

	if err := RemoveTrustedClientEverywhere(stateDir, "sunshine", "c1"); err != nil {
		t.Fatal(err)
	}

	// Simulate the exact reported bug: Sunshine's admin-API unpair call
	// "succeeded" from this layer's point of view (RemoveTrustedClientEverywhere
	// already force-rewrote its file to exclude the client -- that's the
	// fix), but pretend some other path independently re-wrote Sunshine's
	// file with the stale entry still in it (e.g. a race with Sunshine's
	// own live process re-saving its in-memory state, which it never heard
	// about the removal).
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{{Fingerprint: fp, UniqueID: "c1", CertDER: der}}); err != nil {
		t.Fatal(err)
	}

	// Reconciling again (e.g. a later restart) must not propagate the
	// resurrected entry to rust-shine/punktfunk, and should ideally clean
	// it back out of Sunshine's own file too.
	ReconcileSharedAuth(stateDir)

	rsClients, _ := readRustshineTrusted(stateDir)
	pfClients, _ := readPunktfunkTrusted(stateDir)
	if containsFingerprint(rsClients, fp) {
		t.Error("tombstoned client was resurrected into rust-shine's trust file by reconciliation")
	}
	if containsFingerprint(pfClients, fp) {
		t.Error("tombstoned client was resurrected into punktfunk's trust file by reconciliation")
	}
}

// TestSyncAfterPair_PropagatesImmediatelyWithoutWaitingForAnotherStart
// proves pairing propagation doesn't need the other backends to ever call
// ReconcileSharedAuth themselves first -- it happens right after SubmitPIN,
// exactly as app.go's SubmitMoonlightPIN now drives it.
func TestSyncAfterPair_PropagatesImmediatelyWithoutWaitingForAnotherStart(t *testing.T) {
	stateDir := t.TempDir()
	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{{Fingerprint: fp, UniqueID: "c1", CertDER: der}}); err != nil {
		t.Fatal(err)
	}

	SyncAfterPair(stateDir, "sunshine", nil)

	rsClients, _ := readRustshineTrusted(stateDir)
	pfClients, _ := readPunktfunkTrusted(stateDir)
	if !containsFingerprint(rsClients, fp) {
		t.Error("rust-shine did not receive the client immediately after SyncAfterPair")
	}
	if !containsFingerprint(pfClients, fp) {
		t.Error("punktfunk did not receive the client immediately after SyncAfterPair")
	}
}

// TestSyncAfterPair_ClearsTombstoneOnGenuineRePair is the regression test
// for the design gap a prior version of this feature had: a permanent
// tombstone (needed so ReconcileSharedAuth's passive reconcile can never
// resurrect a removed client from a backend's stale file -- see
// TestRemoveTrustedClientEverywhere_FixesTheReportedSunshineCannotDeleteBug)
// must not also permanently block the operator from re-pairing that exact
// same physical device later. SyncAfterPair, called right after a real
// SubmitPIN success, is what's allowed to lift it.
func TestSyncAfterPair_ClearsTombstoneOnGenuineRePair(t *testing.T) {
	stateDir := t.TempDir()
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{{Fingerprint: fp, UniqueID: "c1", CertDER: der}}); err != nil {
		t.Fatal(err)
	}
	ReconcileSharedAuth(stateDir)
	if err := RemoveTrustedClientEverywhere(stateDir, "sunshine", "c1"); err != nil {
		t.Fatal(err)
	}

	store := loadCanonicalStore(stateDir)
	if !containsString(store.Removed, fp) {
		t.Fatalf("expected %s to be tombstoned after removal", fp)
	}

	// The operator re-pairs the identical physical device against
	// rust-shine this time (same cert, since Moonlight reuses its
	// persisted client keypair) -- simulating a real SubmitPIN success by
	// seeding rust-shine's own native file the way its SubmitPIN already
	// would, then calling SyncAfterPair exactly as SubmitMoonlightPIN does.
	if err := writeRustshineTrusted(stateDir, []trustedClient{{Fingerprint: fp, UniqueID: "c1-again", CertDER: der}}); err != nil {
		t.Fatal(err)
	}
	SyncAfterPair(stateDir, "rustshine", nil)

	store = loadCanonicalStore(stateDir)
	if containsString(store.Removed, fp) {
		t.Error("tombstone was not lifted after a genuine re-pair via SyncAfterPair")
	}

	pfClients, _ := readPunktfunkTrusted(stateDir)
	if !containsFingerprint(pfClients, fp) {
		t.Error("re-paired client was not propagated to punktfunk after the tombstone was lifted")
	}
	_, sunClients, _ := readSunshineTrusted(stateDir)
	if !containsFingerprint(sunClients, fp) {
		t.Error("re-paired client was not propagated to sunshine after the tombstone was lifted")
	}
}

// TestReconcileSharedAuth_NeverLiftsATombstoneOnItsOwn is the other half of
// the same guarantee: ReconcileSharedAuth (the passive, every-Start()-call
// path) must never clear a tombstone just because a backend's file still
// happens to contain the fingerprint -- only SyncAfterPair, in direct
// response to a real pairing event, may do that. Without this,
// reconciliation alone could not reliably distinguish "stale leftover data"
// (the reported bug) from "legitimate standing pairing," and the whole
// point of the tombstone would be defeated.
func TestReconcileSharedAuth_NeverLiftsATombstoneOnItsOwn(t *testing.T) {
	stateDir := t.TempDir()
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{{Fingerprint: fp, UniqueID: "c1", CertDER: der}}); err != nil {
		t.Fatal(err)
	}
	ReconcileSharedAuth(stateDir)
	if err := RemoveTrustedClientEverywhere(stateDir, "sunshine", "c1"); err != nil {
		t.Fatal(err)
	}
	// Stale data reappears in Sunshine's own file (the exact bug scenario),
	// with no SyncAfterPair call -- only passive reconciliation runs.
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{{Fingerprint: fp, UniqueID: "c1", CertDER: der}}); err != nil {
		t.Fatal(err)
	}

	for i := 0; i < 3; i++ {
		ReconcileSharedAuth(stateDir)
	}

	store := loadCanonicalStore(stateDir)
	if !containsString(store.Removed, fp) {
		t.Error("ReconcileSharedAuth lifted a tombstone on its own, without a real pairing event -- this would make the unpair bug fix unreliable")
	}
}

// TestRemoveTrustedClientEverywhere_WorksForAClientNeverSyncedBefore covers
// upgrading an existing install: a client paired long before this feature
// existed has no canonical-store entry at all, only a native Sunshine one.
// Unpairing it must still resolve and remove correctly.
func TestRemoveTrustedClientEverywhere_WorksForAClientNeverSyncedBefore(t *testing.T) {
	stateDir := t.TempDir()
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	// Native Sunshine file has the client; canonical store (identity/trusted_clients.json) does not exist at all yet.
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{{Fingerprint: fp, UniqueID: "legacy-client", CertDER: der}}); err != nil {
		t.Fatal(err)
	}

	if err := RemoveTrustedClientEverywhere(stateDir, "sunshine", "legacy-client"); err != nil {
		t.Fatalf("RemoveTrustedClientEverywhere: %v", err)
	}
	_, sunClients, _ := readSunshineTrusted(stateDir)
	if containsFingerprint(sunClients, fp) {
		t.Error("legacy client (never synced into canonical store) was not removed")
	}
}

// TestRemoveTrustedClientEverywhere_PunktfunkIdentifierIsAFingerprintNotAUUID
// exercises punktfunk's different unpair semantics (see UnpairClient's doc
// comment in punktfunk_pairing.go: the identifier IS the fingerprint).
func TestRemoveTrustedClientEverywhere_PunktfunkIdentifierIsAFingerprintNotAUUID(t *testing.T) {
	stateDir := t.TempDir()
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	if err := writePunktfunkTrusted(stateDir, []trustedClient{{Fingerprint: fp, CertDER: der}}); err != nil {
		t.Fatal(err)
	}
	ReconcileSharedAuth(stateDir)

	if err := RemoveTrustedClientEverywhere(stateDir, "punktfunk", fp); err != nil {
		t.Fatalf("RemoveTrustedClientEverywhere: %v", err)
	}
	pfClients, _ := readPunktfunkTrusted(stateDir)
	rsClients, _ := readRustshineTrusted(stateDir)
	if containsFingerprint(pfClients, fp) || containsFingerprint(rsClients, fp) {
		t.Error("fingerprint-identified punktfunk client was not removed everywhere")
	}
}

func TestRemoveTrustedClientEverywhere_UnknownIdentifierReturnsErrorNotPanic(t *testing.T) {
	stateDir := t.TempDir()
	if err := RemoveTrustedClientEverywhere(stateDir, "sunshine", "no-such-client"); err == nil {
		t.Error("expected an error for an unresolvable identifier, got nil")
	}
}

// TestReconcileSharedAuth_CorruptCanonicalStoreDegradesGracefully simulates
// the canonical store file itself being corrupted (disk corruption, an
// interrupted write from an older/future version) -- must never prevent any
// backend from starting, only lose the sync bookkeeping (each backend's own
// native file remains authoritative and unaffected).
func TestReconcileSharedAuth_CorruptCanonicalStoreDegradesGracefully(t *testing.T) {
	stateDir := t.TempDir()
	if _, _, _, err := EnsureSharedIdentity(stateDir); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(sharedIdentityDir(stateDir), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(trustStorePath(stateDir), []byte("{not valid json"), 0o600); err != nil {
		t.Fatal(err)
	}

	// Must not panic and must not block Sunshine's Start() path.
	ReconcileSharedAuth(stateDir)

	// The canonical store should have been rewritten as valid JSON by now.
	data, err := os.ReadFile(trustStorePath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	if len(data) == 0 {
		t.Error("canonical store is empty after recovering from corruption")
	}
}

// TestReconcileSharedAuth_MissingStateDirsAreCreated covers a totally fresh
// install where none of sunshine/, rustshine/, punktfunk/, or identity/
// exist yet -- must not error out, and must create what it needs.
func TestReconcileSharedAuth_MissingStateDirsAreCreated(t *testing.T) {
	stateDir := filepath.Join(t.TempDir(), "does-not-exist-yet")
	ReconcileSharedAuth(stateDir)
	if _, err := os.Stat(sharedCertPath(stateDir)); err != nil {
		t.Errorf("shared identity was not created: %v", err)
	}
}

// TestReconcileSharedAuth_ConcurrentCallsDoNotCorruptFiles exercises the
// cross-process/in-process locking atomicWriteLocked relies on
// (configFileLock/acquireCrossProcessLock, shared with configfile.go) --
// several goroutines reconciling at once (e.g. a rapid backend-switch
// double-click, or the admin HTTP API and a Start() racing) must never
// leave a torn/truncated file behind.
func TestReconcileSharedAuth_ConcurrentCallsDoNotCorruptFiles(t *testing.T) {
	stateDir := t.TempDir()
	der := mustGenCertDER(t)
	fp := fingerprintOf(der)
	_, _, serverUUID, err := EnsureSharedIdentity(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if err := writeSunshineTrusted(stateDir, serverUUID, []trustedClient{{Fingerprint: fp, UniqueID: "c1", CertDER: der}}); err != nil {
		t.Fatal(err)
	}

	done := make(chan struct{})
	for i := 0; i < 8; i++ {
		go func() {
			defer func() { done <- struct{}{} }()
			ReconcileSharedAuth(stateDir)
		}()
	}
	for i := 0; i < 8; i++ {
		<-done
	}

	// Every file touched must still parse cleanly -- a torn write would
	// show up here as a JSON/PEM parse error, not a data-correctness
	// difference.
	if _, _, err := readSunshineTrusted(stateDir); err != nil {
		t.Errorf("sunshine_state.json unreadable after concurrent reconciles: %v", err)
	}
	if _, err := readRustshineTrusted(stateDir); err != nil {
		t.Errorf("trusted_clients.pem unreadable after concurrent reconciles: %v", err)
	}
	if _, err := readPunktfunkTrusted(stateDir); err != nil {
		t.Errorf("paired.json unreadable after concurrent reconciles: %v", err)
	}
}

// TestBackendKind_IdentifiesAllThreeConcreteBackends pins the mapping
// RemoveTrustedClientEverywhere's caller (app.go's UnpairSunshineClient)
// relies on -- a typo or a renamed type here would silently stop
// propagating removals for that backend.
func TestBackendKind_IdentifiesAllThreeConcreteBackends(t *testing.T) {
	cases := []struct {
		backend Backend
		want    string
	}{
		{NewSunshine("", "", ""), "sunshine"},
		{NewRustshine("", "", ""), "rustshine"},
		{NewPunktfunk("", "", ""), "punktfunk"},
	}
	for _, c := range cases {
		if got := BackendKind(c.backend); got != c.want {
			t.Errorf("BackendKind(%T) = %q, want %q", c.backend, got, c.want)
		}
	}
}

func containsFingerprint(clients []trustedClient, fp string) bool {
	return countFingerprint(clients, fp) > 0
}

func countFingerprint(clients []trustedClient, fp string) int {
	n := 0
	for _, c := range clients {
		if c.Fingerprint == fp {
			n++
		}
	}
	return n
}

// TestAwaitPairingAndSync_WaitsForTheHandshakeToFinish is the regression
// test for the bug where SyncAfterPair ran straight after SubmitPIN: the
// host writes the new client's certificate only after the client's later
// pairing stages, so the sync saw nothing, the tombstone stayed, and the
// next backend Start() stripped the fresh pairing everywhere. A stale
// tombstoned entry already in the file before the PIN must stay removed.
func TestAwaitPairingAndSync_WaitsForTheHandshakeToFinish(t *testing.T) {
	stateDir := t.TempDir()
	if _, _, _, err := EnsureSharedIdentity(stateDir); err != nil {
		t.Fatal(err)
	}
	staleDER, freshDER := mustGenCertDER(t), mustGenCertDER(t)
	staleFP, freshFP := fingerprintOf(staleDER), fingerprintOf(freshDER)
	if err := saveCanonicalStore(stateDir, trustStoreFile{Removed: []string{staleFP, freshFP}}); err != nil {
		t.Fatal(err)
	}
	stale := trustedClient{Fingerprint: staleFP, CertDER: staleDER}
	if err := writePunktfunkTrusted(stateDir, []trustedClient{stale}); err != nil {
		t.Fatal(err)
	}

	before := TrustedFingerprints(stateDir, "punktfunk")
	go func() {
		time.Sleep(300 * time.Millisecond)
		_ = writePunktfunkTrusted(stateDir, []trustedClient{stale, {Fingerprint: freshFP, CertDER: freshDER}})
	}()
	if !AwaitPairingAndSync(stateDir, "punktfunk", before, 5*time.Second) {
		t.Fatal("did not see the client that paired after SubmitPIN returned")
	}

	store := loadCanonicalStore(stateDir)
	if containsString(store.Removed, freshFP) {
		t.Error("tombstone on the freshly re-paired client was not lifted")
	}
	if !containsString(store.Removed, staleFP) {
		t.Error("stale entry that predates the PIN had its tombstone lifted")
	}
	rs, _ := readRustshineTrusted(stateDir)
	_, sun, _ := readSunshineTrusted(stateDir)
	if !containsFingerprint(rs, freshFP) || !containsFingerprint(sun, freshFP) {
		t.Error("freshly paired client was not propagated to rust-shine and sunshine")
	}
	if containsFingerprint(rs, staleFP) || containsFingerprint(sun, staleFP) {
		t.Error("stale tombstoned client was propagated")
	}
}
