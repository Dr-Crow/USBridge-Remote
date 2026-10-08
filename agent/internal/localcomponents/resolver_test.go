package localcomponents

import (
	"archive/zip"
	"context"
	"encoding/json"
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func fixture(t *testing.T, version string) (string, []byte) {
	t.Helper()
	dir := t.TempDir()
	data := []byte("non-executable test fixture " + version)
	m := Manifest{Schema: 1, Components: []Component{{Name: "streamer", Platform: runtime.GOOS + "/" + runtime.GOARCH, Version: version, Profile: "fixture", Entry: "bin/streamer", Files: []File{{Path: "bin/streamer", Size: int64(len(data)), SHA256: digest(data)}}}}}
	raw, _ := json.Marshal(m)
	if err := os.MkdirAll(filepath.Join(dir, "bin"), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bin/streamer"), data, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	return dir, raw
}
func TestStageReuseRejectTamperingAndPreservePriorVersion(t *testing.T) {
	dir, raw := fixture(t, "1")
	state := t.TempDir()
	o := Options{StateDir: state, Directory: dir, ManifestSHA256: digest(raw)}
	r, err := Resolve(context.Background(), o, "streamer")
	if err != nil {
		t.Fatal(err)
	}
	if r.Version != "1" {
		t.Fatal(r)
	}
	if _, err := Resolve(context.Background(), Options{StateDir: state, ManifestSHA256: digest(raw)}, "streamer"); err != nil {
		t.Fatalf("installed reuse: %v", err)
	}
	newDir, newRaw := fixture(t, "2")
	o.Directory = newDir
	o.ManifestSHA256 = digest(newRaw)
	// Corrupt selected source must leave the old active component intact.
	if err := os.WriteFile(filepath.Join(newDir, "bin/streamer"), []byte("corrupted"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(context.Background(), o, "streamer"); err == nil {
		t.Fatal("corrupt source staged")
	}
	if b, _ := os.ReadFile(r.Binary); string(b) != "non-executable test fixture 1" {
		t.Fatal("prior component lost")
	}
	newDir, newRaw = fixture(t, "2")
	o.Directory = newDir
	o.ManifestSHA256 = digest(newRaw)
	next, err := Resolve(context.Background(), o, "streamer")
	if err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(next.PreviousDirectory, "bin/streamer")); string(b) != "non-executable test fixture 1" {
		t.Fatal("rollback copy missing")
	}
	if err := os.WriteFile(next.Binary, []byte("changed after installation"), 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(context.Background(), Options{StateDir: state, ManifestSHA256: digest(newRaw)}, "streamer"); err == nil {
		t.Fatal("tampered installed component accepted")
	}
}
func TestManifestRejectsArchitectureTraversalDuplicateMissingEntry(t *testing.T) {
	_, raw := fixture(t, "1")
	for _, mutate := range []func(*Manifest){
		func(m *Manifest) { m.Components[0].Platform = "wrong/architecture" },
		func(m *Manifest) { m.Components[0].Files[0].Path = "../outside" },
		func(m *Manifest) { m.Components[0].Entry = "absent" },
		func(m *Manifest) { m.Components[0].Files = append(m.Components[0].Files, m.Components[0].Files[0]) },
		func(m *Manifest) { m.Schema = 99 },
	} {
		var m Manifest
		_ = json.Unmarshal(raw, &m)
		mutate(&m)
		b, _ := json.Marshal(m)
		if _, err := component(b, "streamer", runtime.GOOS+"/"+runtime.GOARCH, ""); err == nil {
			t.Fatal("invalid manifest accepted")
		}
	}
	if _, err := component(raw, "streamer", runtime.GOOS+"/"+runtime.GOARCH, digest([]byte("other"))); err == nil {
		t.Fatal("incorrect manifest pin accepted")
	}
}
func TestDirectoryRejectsSymlinks(t *testing.T) {
	dir, _ := fixture(t, "1")
	if err := os.Remove(filepath.Join(dir, "bin/streamer")); err != nil {
		t.Fatal(err)
	}
	external := filepath.Join(t.TempDir(), "external")
	_ = os.WriteFile(external, []byte("non-executable test fixture 1"), 0600)
	if err := os.Symlink(external, filepath.Join(dir, "bin/streamer")); err != nil {
		t.Fatal(err)
	}
	if _, err := Resolve(context.Background(), Options{StateDir: t.TempDir(), Directory: dir}, "streamer"); err == nil {
		t.Fatal("symlink accepted")
	}
}
func zipFixture(t *testing.T, dir, extra string, symlink bool) string {
	t.Helper()
	p := filepath.Join(t.TempDir(), "bundle.zip")
	f, err := os.Create(p)
	if err != nil {
		t.Fatal(err)
	}
	z := zip.NewWriter(f)
	for _, n := range []string{"manifest.json", "bin/streamer"} {
		w, e := z.Create(n)
		if e != nil {
			t.Fatal(e)
		}
		b, e := os.ReadFile(filepath.Join(dir, n))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = w.Write(b); e != nil {
			t.Fatal(e)
		}
	}
	if extra != "" {
		h := &zip.FileHeader{Name: extra}
		h.SetMode(0600)
		if symlink {
			h.SetMode(os.ModeSymlink | 0600)
		}
		w, e := z.CreateHeader(h)
		if e != nil {
			t.Fatal(e)
		}
		_, _ = w.Write([]byte("link"))
	}
	if err := z.Close(); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	return p
}
func TestBundleStagesAndRejectsUnsafeEntries(t *testing.T) {
	dir, _ := fixture(t, "1")
	p := zipFixture(t, dir, "", false)
	if _, err := Resolve(context.Background(), Options{StateDir: t.TempDir(), Bundle: p}, "streamer"); err != nil {
		t.Fatal(err)
	}
	for _, n := range []string{"../outside", "/absolute", "C:\\escape", "manifest.json"} {
		p := zipFixture(t, dir, n, false)
		if _, err := openBundle(p); err == nil {
			t.Fatalf("accepted ZIP path %q", n)
		}
	}
	if _, err := openBundle(zipFixture(t, dir, "link", true)); err == nil {
		t.Fatal("ZIP symlink accepted")
	}
}
func TestMirrorRejectsPublicDNSAndMissingPin(t *testing.T) {
	for _, o := range []Options{{Mirror: "https://example.invalid/", ManifestSHA256: digest(nil)}, {Mirror: "https://8.8.8.8/", ManifestSHA256: digest(nil)}, {Mirror: "https://192.168.1.1/"}} {
		if s, err := selectedSource(o); err == nil {
			s.close()
			t.Fatal("invalid mirror accepted")
		}
	}
}

func TestCancelledResolutionDoesNotCreateStaging(t *testing.T) {
	dir, _ := fixture(t, "1")
	state := t.TempDir()
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Resolve(ctx, Options{StateDir: state, Directory: dir}, "streamer"); err != context.Canceled {
		t.Fatalf("cancel: %v", err)
	}
	if _, err := os.Stat(filepath.Join(state, "local-components")); !os.IsNotExist(err) {
		t.Fatal("cancelled resolution created staging")
	}
}

func TestPortablePathsRejectWindowsAliasesAndEncodedTraversal(t *testing.T) {
	for _,p:=range []string{"CON","bin/NUL.txt","COM1.exe","LPT9","file.","file ","bin/%2e%2e/escape"}{if safePath(p){t.Fatalf("unsafe portable path %q",p)}}
	_,raw:=fixture(t,"1");var m Manifest;_ = json.Unmarshal(raw,&m);f:=m.Components[0].Files[0];f.Path="BIN/STREAMER";m.Components[0].Files=append(m.Components[0].Files,f);raw,_=json.Marshal(m)
	if _,err:=component(raw,"streamer",runtime.GOOS+"/"+runtime.GOARCH,"");err==nil{t.Fatal("case-insensitive file collision accepted")}
}

func TestPinnedTLSMirrorStagesAndRejectsWrongPin(t *testing.T) {
	dir, raw := fixture(t, "mirror")
	srv := httptest.NewTLSServer(http.FileServer(http.Dir(dir)))
	t.Cleanup(srv.Close)
	ca := filepath.Join(t.TempDir(), "ca.pem")
	if err := os.WriteFile(ca, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw}), 0600); err != nil {
		t.Fatal(err)
	}
	o := Options{StateDir: t.TempDir(), Mirror: srv.URL, CAFile: ca, ManifestSHA256: digest(raw)}
	if r, err := Resolve(context.Background(), o, "streamer"); err != nil || r.Version != "mirror" {
		t.Fatalf("result=%+v error=%v", r, err)
	}
	o.StateDir = t.TempDir()
	o.ManifestSHA256 = digest([]byte("wrong"))
	if _, err := Resolve(context.Background(), o, "streamer"); err == nil {
		t.Fatal("wrong mirror manifest pin accepted")
	}
	o.StateDir = t.TempDir()
	o.CAFile = ""
	o.ManifestSHA256 = digest(raw)
	if _, err := Resolve(context.Background(), o, "streamer"); err == nil {
		t.Fatal("untrusted mirror TLS accepted")
	}
}
