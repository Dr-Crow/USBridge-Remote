package streamhost

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// realSunshineStateJSON is a byte-for-byte capture of a real
// sunshine_state.json from this agent's own live data directory (4 real
// paired Moonlight clients) -- a golden fixture so this parser's
// understanding of the on-disk shape can never silently regress, even if
// nobody remembers to check it against a real file again.
const realSunshineStateJSON = `{
    "username": "sunshine",
    "salt": "ixXn6kHf9-FBTI5T",
    "password": "7F71A87BE51653643E4AA1E85724F20A268CA10FE3700D529C9952E8F81FC873",
    "root": {
        "uniqueid": "C064935C-8F6E-E601-D90C-41DCF049EFA9",
        "named_devices": [
            {
                "name": "",
                "cert": "-----BEGIN CERTIFICATE-----\nMIIC/TCCAeWgAwIBAgIIGMNjL1uXBhcwDQYJKoZIhvcNAQELBQAwIzEhMB8GA1UE\nAxMYTlZJRElBIEdhbWVTdHJlYW0gQ2xpZW50MB4XDTI2MDcxNzEyNDg1MloXDTQ2\nMDcxMzEyNDg1MlowIzEhMB8GA1UEAxMYTlZJRElBIEdhbWVTdHJlYW0gQ2xpZW50\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAtmiQJ0MXxQDIUKifYTiW\nuY7Lo//rvXOQWdgNgWsA3pKhEvMUIj1WryuS75YZW/Vken7uW68EsKO4nLt/Cbym\n9s8eAMuCStOnHOUnQt58C57vKbyL9xOBHa6KYQULtF13iDkevOPqmZPEuP8uKrET\nJR+gMtFXkMInr9oQlY9VT/q5xW7y1kRhL9cKBnGRjuvwHi+FOdSb+LfaZSBnFQmV\nVFcPFHhF2CnGVwpZ+NTWUm0uIaEQvH0g1Krsbj6/8+X5FVzYQb3JUwNpSCVit4Yy\ndUUpivBBUsIlZl4fsaJ/SlWrpDc932ZnFqIuzS4dfmgkZA4HjKgl0eLXcQElDKX4\nBQIDAQABozUwMzAOBgNVHQ8BAf8EBAMCBaAwEwYDVR0lBAwwCgYIKwYBBQUHAwIw\nDAYDVR0TAQH/BAIwADANBgkqhkiG9w0BAQsFAAOCAQEAiEXtOTDo4tUM7g0eBv64\nEvvob0qFSXLuCKVjPrs7grg0Mjt1eouygwvMkN6hShTMhb/UannKfCi27T134t1G\nIAuywRQmo3UqIu/PO+Jt3eVVT2ybzGCKNoWOOjVz13XOxMhuY5s3ABdwFBs4xac1\nqgZpQdKKOvIB3RS1AHA/U9tRec1ECxdcf1iFpPy9TeS3HdtBMVUkNx67m9GWEF++\nPSpXInE16IPCsfrdxzQ8bjgALeq6ro/KJoPi4mnE+qde7aUaTBedTU87h3Cmb7dn\nntJ1aifZGYMeirqy4vxW090vNWH4Rd9pwv9xXD5lGzQgaaEjh6U5lR0qIqR/FZQL\ntg==\n-----END CERTIFICATE-----\n",
                "uuid": "2018A71E-A343-9C27-E110-74F248BA06E3",
                "enabled": "true"
            },
            {
                "name": "",
                "cert": "-----BEGIN CERTIFICATE-----\nMIIC/TCCAeWgAwIBAgIIGMQF3lUPuDwwDQYJKoZIhvcNAQELBQAwIzEhMB8GA1UE\nAxMYTlZJRElBIEdhbWVTdHJlYW0gQ2xpZW50MB4XDTI2MDcxOTE0MzAwNFoXDTQ2\nMDcxNTE0MzAwNFowIzEhMB8GA1UEAxMYTlZJRElBIEdhbWVTdHJlYW0gQ2xpZW50\nMIIBIjANBgkqhkiG9w0BAQEFAAOCAQ8AMIIBCgKCAQEAspb1lkvwiv8wgTEDstcj\nj1/LhbBiZuwEnlcobvk/U7lbMNKlVX6MP23drpIMDYqdCTb4ql/Mw3NvV0BhwEip\np8pDPExqP5a9AOH3BN/Nd5EqnqKY/TrCbtYl9CkjtLBDIH0tA7cuaUlELdLLgEPd\nUwhFmGoUU/2w2tPjB7vsDNlt0Q7x7S5YQL1U9iwVD7NnOePtOrShsbqwbxhvdueV\nM3TTd5JDJqQkHI5TmTLxBkQ9HTXOvfE/NbecauSRWfSo64vo4KETIP2b/rEcwE6Q\nuW7f7SzkS+Tq71miJzrD4wS/PTHUgv/j2cgKezHtWoUL7cO7k2Nak11YqG9gMUT4\n2QIDAQABozUwMzAOBgNVHQ8BAf8EBAMCBaAwEwYDVR0lBAwwCgYIKwYBBQUHAwIw\nDAYDVR0TAQH/BAIwADANBgkqhkiG9w0BAQsFAAOCAQEAgqNiE1Dq7CvryTl1x2wR\nsKCuOCOyz0pB8j5h3kVphCtr2jtmsabV422O0EX9QXQEk7TQE8BQTBYMid/iQBNZ\nr1Lwry/e3C2bHsQqkJwjXA27zfOWIlS+oToZXI2jbbMUKxgH5KGZnL3AXXnBVgue\ngDWxTQV05D1cK1FtrBjoqt9cHZ8anLzpz2RsQxaW6tXKAFXeK1KWMrMUf0bBZOkL\ngAOrSXVN9smyGIkSTcO2SXfbQDHAQgpA/VaLEp8ESNMaEsZ7mDsJWk4b1Xi3jOx8\nd5GoYdftmG+sv+YZNbIK4ilCRLm1UIE5ANLz65x1WLkS7OYqg4rBiIs9lurB9Jwa\n9w==\n-----END CERTIFICATE-----\n",
                "uuid": "39629B11-9286-9C0F-4612-AF8A8B228102",
                "enabled": "true"
            }
        ]
    }
}
`

func TestReadSunshineTrusted_ParsesRealCapturedStateFile(t *testing.T) {
	stateDir := t.TempDir()
	sunDir := filepath.Join(stateDir, "sunshine")
	if err := os.MkdirAll(sunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sunDir, "sunshine_state.json"), []byte(realSunshineStateJSON), 0o644); err != nil {
		t.Fatal(err)
	}

	uuid, clients, err := readSunshineTrusted(stateDir)
	if err != nil {
		t.Fatalf("readSunshineTrusted: %v", err)
	}
	if uuid != "C064935C-8F6E-E601-D90C-41DCF049EFA9" {
		t.Errorf("uuid = %q", uuid)
	}
	if len(clients) != 2 {
		t.Fatalf("expected 2 paired clients from the real fixture, got %d", len(clients))
	}
	wantUUIDs := map[string]bool{"2018A71E-A343-9C27-E110-74F248BA06E3": true, "39629B11-9286-9C0F-4612-AF8A8B228102": true}
	for _, c := range clients {
		if !wantUUIDs[c.UniqueID] {
			t.Errorf("unexpected client uuid %q", c.UniqueID)
		}
		if len(c.CertDER) == 0 {
			t.Errorf("client %s has no decoded cert DER", c.UniqueID)
		}
		if c.Fingerprint == "" {
			t.Errorf("client %s has no computed fingerprint", c.UniqueID)
		}
		if c.Enabled != "true" {
			t.Errorf("client %s enabled = %q, want \"true\"", c.UniqueID, c.Enabled)
		}
	}
}

func TestWriteSunshineTrusted_PreservesUsernameSaltPasswordAndUnknownFields(t *testing.T) {
	stateDir := t.TempDir()
	sunDir := filepath.Join(stateDir, "sunshine")
	if err := os.MkdirAll(sunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	// Inject an extra, unknown top-level and root field to prove they
	// survive a round trip -- a future Sunshine version (or this fork)
	// could add fields this code has never heard of, and a write must
	// never silently drop them.
	seeded := `{"username":"sunshine","salt":"realsalt","password":"realhash","future_top_level_field":42,"root":{"uniqueid":"OLD-UUID","named_devices":[],"future_root_field":"keep-me"}}`
	path := filepath.Join(sunDir, "sunshine_state.json")
	if err := os.WriteFile(path, []byte(seeded), 0o644); err != nil {
		t.Fatal(err)
	}

	client := trustedClient{Fingerprint: "deadbeef", UniqueID: "client-1", CertDER: mustGenCertDER(t)}
	if err := writeSunshineTrusted(stateDir, "NEW-UUID", []trustedClient{client}); err != nil {
		t.Fatalf("writeSunshineTrusted: %v", err)
	}

	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var top map[string]any
	if err := json.Unmarshal(data, &top); err != nil {
		t.Fatal(err)
	}
	if top["username"] != "sunshine" || top["salt"] != "realsalt" || top["password"] != "realhash" {
		t.Errorf("username/salt/password were not preserved: %+v", top)
	}
	if top["future_top_level_field"] != float64(42) {
		t.Errorf("unknown top-level field was dropped: %+v", top)
	}
	root, _ := top["root"].(map[string]any)
	if root == nil {
		t.Fatalf("root missing entirely")
	}
	if root["uniqueid"] != "NEW-UUID" {
		t.Errorf("root.uniqueid = %v, want NEW-UUID", root["uniqueid"])
	}
	if root["future_root_field"] != "keep-me" {
		t.Errorf("unknown root field was dropped: %+v", root)
	}

	// Round-trip back through the reader.
	_, clients, err := readSunshineTrusted(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(clients) != 1 || clients[0].UniqueID != "client-1" {
		t.Errorf("round-tripped clients = %+v", clients)
	}
}

func TestReadSunshineTrusted_MissingOrCorruptFileDegradesToEmptyNotError(t *testing.T) {
	stateDir := t.TempDir()
	if uuid, clients, err := readSunshineTrusted(stateDir); err != nil || uuid != "" || clients != nil {
		t.Errorf("missing file: got uuid=%q clients=%v err=%v, want empty/nil/nil", uuid, clients, err)
	}

	sunDir := filepath.Join(stateDir, "sunshine")
	if err := os.MkdirAll(sunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(sunDir, "sunshine_state.json"), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}
	if uuid, clients, err := readSunshineTrusted(stateDir); err != nil || uuid != "" || clients != nil {
		t.Errorf("corrupt file: got uuid=%q clients=%v err=%v, want empty/nil/nil (never fatal)", uuid, clients, err)
	}
}

func TestReadSunshineTrusted_OneMalformedCertDoesNotBlockTheOthers(t *testing.T) {
	stateDir := t.TempDir()
	sunDir := filepath.Join(stateDir, "sunshine")
	if err := os.MkdirAll(sunDir, 0o755); err != nil {
		t.Fatal(err)
	}
	goodDER := mustGenCertDER(t)
	good := derToPEM(goodDER)
	escaped := strings.ReplaceAll(good, "\n", "\\n")
	content := `{"root":{"uniqueid":"X","named_devices":[` +
		`{"name":"","cert":"not a real pem","uuid":"bad-client","enabled":"true"},` +
		`{"name":"","cert":"` + escaped + `","uuid":"good-client","enabled":"true"}` +
		`]}}`
	if err := os.WriteFile(filepath.Join(sunDir, "sunshine_state.json"), []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}

	_, clients, err := readSunshineTrusted(stateDir)
	if err != nil {
		t.Fatalf("readSunshineTrusted: %v", err)
	}
	if len(clients) != 1 || clients[0].UniqueID != "good-client" {
		t.Errorf("expected only the valid entry to survive, got %+v", clients)
	}
}

// --- rust-shine trusted_clients.pem --------------------------------------

func TestRustshineTrustedClientsPEM_RoundTrips(t *testing.T) {
	der1, der2 := mustGenCertDER(t), mustGenCertDER(t)
	clients := []trustedClient{
		{Fingerprint: fingerprintOf(der1), UniqueID: "client-a", CertDER: der1},
		{Fingerprint: fingerprintOf(der2), UniqueID: "client-b", CertDER: der2},
	}
	encoded := encodeTrustedClientsPEM(clients)
	uniqueids, ders := parseTrustedClientsPEM(encoded)
	if len(ders) != 2 {
		t.Fatalf("expected 2 certs, got %d", len(ders))
	}
	got := map[string]bool{}
	for i, der := range ders {
		got[uniqueids[i]+":"+fingerprintOf(der)] = true
	}
	for _, c := range clients {
		if !got[c.UniqueID+":"+c.Fingerprint] {
			t.Errorf("round trip lost %s", c.UniqueID)
		}
	}
}

func TestParseTrustedClientsPEM_FileWithoutUniqueidCommentsStillParses(t *testing.T) {
	// Mirrors rust-shine's own doc comment: a trusted_clients.pem written
	// before the "# uniqueid:" comment format existed must still parse,
	// just with an empty uniqueid per cert.
	der := mustGenCertDER(t)
	legacy := derToPEM(der) // no "# uniqueid:" line at all
	uniqueids, ders := parseTrustedClientsPEM([]byte(legacy))
	if len(ders) != 1 {
		t.Fatalf("expected 1 cert from a legacy-format file, got %d", len(ders))
	}
	if uniqueids[0] != "" {
		t.Errorf("uniqueid = %q, want empty for a legacy entry", uniqueids[0])
	}
}

func TestReadRustshineTrusted_MissingFileDegradesToEmpty(t *testing.T) {
	stateDir := t.TempDir()
	clients, err := readRustshineTrusted(stateDir)
	if err != nil || clients != nil {
		t.Errorf("got clients=%v err=%v, want nil/nil", clients, err)
	}
}

// --- punktfunk paired.json + client-labels.json --------------------------

func TestPunktfunkTrusted_RoundTripsDERArrayAndLabels(t *testing.T) {
	stateDir := t.TempDir()
	der1, der2 := mustGenCertDER(t), mustGenCertDER(t)
	clients := []trustedClient{
		{Fingerprint: fingerprintOf(der1), Name: "Living Room PC", CertDER: der1},
		{Fingerprint: fingerprintOf(der2), CertDER: der2}, // no label
	}
	if err := writePunktfunkTrusted(stateDir, clients); err != nil {
		t.Fatalf("writePunktfunkTrusted: %v", err)
	}

	// paired.json must be a bare JSON array of byte-number arrays (confirmed
	// serde_json::to_vec(Vec<Vec<u8>>) shape) -- not base64, not wrapped in
	// an object.
	raw, err := os.ReadFile(punktfunkPairedPath(stateDir))
	if err != nil {
		t.Fatal(err)
	}
	var asIntArrays [][]int
	if err := json.Unmarshal(raw, &asIntArrays); err != nil {
		t.Fatalf("paired.json is not a bare array of byte-number arrays: %v (content: %s)", err, raw)
	}
	if len(asIntArrays) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(asIntArrays))
	}

	got, err := readPunktfunkTrusted(stateDir)
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 clients back, got %d", len(got))
	}
	byFP := map[string]trustedClient{}
	for _, c := range got {
		byFP[c.Fingerprint] = c
	}
	if byFP[fingerprintOf(der1)].Name != "Living Room PC" {
		t.Errorf("label did not round-trip: %+v", byFP[fingerprintOf(der1)])
	}
	if byFP[fingerprintOf(der2)].Name != "" {
		t.Errorf("unexpected label on the unlabeled client: %+v", byFP[fingerprintOf(der2)])
	}
}

func TestPunktfunkTrusted_CorruptLabelsFileDegradesToNoNamesNotFailure(t *testing.T) {
	stateDir := t.TempDir()
	der := mustGenCertDER(t)
	if err := writePunktfunkTrusted(stateDir, []trustedClient{{Fingerprint: fingerprintOf(der), Name: "X", CertDER: der}}); err != nil {
		t.Fatal(err)
	}
	// Corrupt just the labels sidecar -- paired.json (the actual trust
	// decision) must still read back fine, per punktfunk's own documented
	// discipline ("a corrupt labels file must never lock anyone out").
	if err := os.WriteFile(punktfunkLabelsPath(stateDir), []byte("{not json"), 0o644); err != nil {
		t.Fatal(err)
	}

	clients, err := readPunktfunkTrusted(stateDir)
	if err != nil {
		t.Fatalf("readPunktfunkTrusted should degrade, not fail: %v", err)
	}
	if len(clients) != 1 || clients[0].Name != "" {
		t.Errorf("expected the client to still be trusted with no name, got %+v", clients)
	}
}

func mustGenCertDER(t *testing.T) []byte {
	t.Helper()
	certPEM, _, err := generateIdentity()
	if err != nil {
		t.Fatalf("generateIdentity: %v", err)
	}
	der, err := pemToDER(string(certPEM))
	if err != nil {
		t.Fatalf("pemToDER: %v", err)
	}
	return der
}
