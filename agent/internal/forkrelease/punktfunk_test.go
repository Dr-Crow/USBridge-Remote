package forkrelease

import (
	"bytes"
	"context"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// testdata/manifest.json(.sig): Streamers-Forks v2026.1004.1.usbridge's real
// signed manifest -- the bytes must stay exactly as published (also CRLF-free:
// see .gitattributes), or the signature no longer matches.
func serveRelease(t *testing.T, manifest, sig []byte, assets map[string][]byte) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		name := strings.TrimPrefix(r.URL.Path, "/")
		switch name {
		case manifestName:
			w.Write(manifest)
		case manifestName + ".sig":
			w.Write(sig)
		default:
			if b, ok := assets[name]; ok {
				w.Write(b)
				return
			}
			http.NotFound(w, r)
		}
	}))
	t.Cleanup(srv.Close)
	old := baseURL
	baseURL = srv.URL + "/"
	t.Cleanup(func() { baseURL = old })
}

func readTestdata(t *testing.T) (manifest, sig []byte) {
	t.Helper()
	m, err := os.ReadFile("testdata/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	s, err := os.ReadFile("testdata/manifest.json.sig")
	if err != nil {
		t.Fatal(err)
	}
	return m, s
}

func TestSignedManifestAccepted(t *testing.T) {
	if PunktfunkAssetName() == "" {
		t.Skip("no punktfunk-host build for this platform")
	}
	m, s := readTestdata(t)
	serveRelease(t, m, s, nil)
	v, err := LatestPunktfunkVersion(context.Background())
	if err != nil {
		t.Fatalf("genuine manifest rejected: %v", err)
	}
	if v != "v2026.1004.1.usbridge" {
		t.Fatalf("version %q", v)
	}
}

func TestTamperedManifestRejected(t *testing.T) {
	if PunktfunkAssetName() == "" {
		t.Skip("no punktfunk-host build for this platform")
	}
	m, s := readTestdata(t)
	// Point this platform's asset at another hash: one changed byte.
	bad := bytes.Replace(m, []byte("8cfb943d"), []byte("0cfb943d"), 1)
	bad = bytes.Replace(bad, []byte("73337448"), []byte("03337448"), 1)
	serveRelease(t, bad, s, nil)
	if _, err := LatestPunktfunkVersion(context.Background()); err == nil || !strings.Contains(err.Error(), "signature") {
		t.Fatalf("tampered manifest: err = %v, want a signature failure", err)
	}
	dir := t.TempDir()
	if _, err := PreparePunktfunk(context.Background(), dir, nil); err == nil {
		t.Fatal("tampered manifest staged a build")
	}
	if PunktfunkStaged(dir) {
		t.Fatal("something was put in place")
	}
}

// A genuine manifest, but the archive served is not the one it lists.
func TestSwappedArchiveRejected(t *testing.T) {
	asset := PunktfunkAssetName()
	if asset == "" {
		t.Skip("no punktfunk-host build for this platform")
	}
	m, s := readTestdata(t)
	serveRelease(t, m, s, map[string][]byte{asset: []byte("not the signed build")})
	dir := t.TempDir()
	_, err := PreparePunktfunk(context.Background(), dir, nil)
	if err == nil || !strings.Contains(err.Error(), "SHA-256 mismatch") {
		t.Fatalf("swapped archive: err = %v, want a SHA-256 mismatch", err)
	}
	if PunktfunkStaged(dir) {
		t.Fatal("something was put in place")
	}
	if _, err := os.Stat(filepath.Join(dir, "punktfunk.next")); !os.IsNotExist(err) {
		t.Fatal("left a punktfunk.next behind")
	}
}

func TestSafeName(t *testing.T) {
	for in, want := range map[string]string{
		"punktfunk-host.exe":     "punktfunk-host.exe",
		"./punktfunk-host":       "punktfunk-host",
		"../../evil":             "evil",
		`..\..\Windows\evil.dll`: "evil.dll",
		"dir/punktfunk-host":     "punktfunk-host",
	} {
		if got, ok := safeName(in); !ok || got != want {
			t.Errorf("safeName(%q) = %q, %v; want %q", in, got, ok, want)
		}
	}
	if _, ok := safeName(".."); ok {
		t.Error(`safeName("..") accepted`)
	}
}

// The real thing, from GitHub: USBRIDGE_LIVE_FORKRELEASE=1 go test ./internal/forkrelease -run Live
func TestLiveDownload(t *testing.T) {
	if os.Getenv("USBRIDGE_LIVE_FORKRELEASE") == "" {
		t.Skip("set USBRIDGE_LIVE_FORKRELEASE=1 to download from GitHub")
	}
	dir := t.TempDir()
	// An older build already in place: the update path swaps it out whole.
	old := PunktfunkDir(dir)
	if err := os.MkdirAll(old, 0o755); err != nil {
		t.Fatal(err)
	}
	os.WriteFile(filepath.Join(old, punktfunkBinaryName()), []byte("old"), 0o755)
	os.WriteFile(filepath.Join(old, "VERSION"), []byte("v0-old\n"), 0o644)
	os.WriteFile(filepath.Join(old, "stale-file"), []byte("x"), 0o644)
	latest, err := LatestPunktfunkVersion(context.Background())
	if err != nil || latest == "" {
		t.Fatalf("latest = %q, %v", latest, err)
	}
	prep, err := PreparePunktfunk(context.Background(), dir, nil)
	if err != nil {
		t.Fatal(err)
	}
	if PunktfunkStagedVersion(dir) != "v0-old" {
		t.Fatal("Prepare touched the live build before Commit")
	}
	if err := prep.Commit(); err != nil {
		t.Fatal(err)
	}
	if !PunktfunkStaged(dir) || PunktfunkStagedVersion(dir) != prep.Version || prep.Version != latest {
		t.Fatalf("staged=%v version=%q want %q", PunktfunkStaged(dir), PunktfunkStagedVersion(dir), prep.Version)
	}
	if _, err := os.Stat(filepath.Join(old, "stale-file")); !os.IsNotExist(err) {
		t.Fatal("the old build's files survived the swap")
	}
	if fi, err := os.Stat(filepath.Join(old, punktfunkBinaryName())); err != nil || fi.Size() < 1<<20 {
		t.Fatalf("staged binary looks wrong: %v", err)
	}
	t.Logf("punktfunk-host %s staged in %s", prep.Version, PunktfunkDir(dir))
}
