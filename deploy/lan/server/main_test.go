package main

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fixture(t *testing.T) (string, string) {
	t.Helper()
	web := t.TempDir()
	mirror := t.TempDir()
	for _, p := range []string{"index.html", "gui.html", "app.wasm", "wasm_exec.js", "runtime-policy.js", "bootstrap.js", "ai_vision.js", "models/a.onnx", "vendor/ort/ort.mjs"} {
		os.MkdirAll(filepath.Dir(filepath.Join(web, p)), 0755)
		if err := os.WriteFile(filepath.Join(web, p), []byte("asset"), 0644); err != nil {
			t.Fatal(err)
		}
	}
	sum := sha256.Sum256([]byte("verified"))
	m := manifest{Schema: 1, Components: []component{{Name: "fixture", Platform: "linux/amd64", Version: "v1", Profile: "test", Entry: "fixture.bin", Files: []filePin{{Path: "fixture.bin", Size: 8, SHA256: hex.EncodeToString(sum[:])}}}}}
	b, _ := json.Marshal(m)
	os.WriteFile(filepath.Join(mirror, "manifest.json"), b, 0644)
	os.WriteFile(filepath.Join(mirror, "fixture.bin"), []byte("verified"), 0644)
	os.WriteFile(filepath.Join(mirror, "private.key"), []byte("must-not-serve"), 0600)
	return web, mirror
}
func TestTLSHostAssetsPolicyAndMirror(t *testing.T) {
	web, mirror := fixture(t)
	h, err := newHost(web, mirror, []string{"https://192.168.1.8:8443"})
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	srv := httptest.NewTLSServer(h)
	defer srv.Close()
	for _, tc := range []struct{ path, mime string }{{"/app.wasm", "application/wasm"}, {"/vendor/ort/ort.mjs", "text/javascript"}, {"/models/a.onnx", "application/octet-stream"}, {"/components/fixture.bin", ""}} {
		resp, err := srv.Client().Get(srv.URL + tc.path)
		if err != nil {
			t.Fatal(err)
		}
		io.Copy(io.Discard, resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 {
			t.Fatalf("%s: %d", tc.path, resp.StatusCode)
		}
		if tc.mime != "" && !strings.HasPrefix(resp.Header.Get("Content-Type"), tc.mime) {
			t.Fatal(resp.Header)
		}
		csp := resp.Header.Get("Content-Security-Policy")
		if !strings.Contains(csp, "connect-src 'self' https://192.168.1.8:8443") || strings.Contains(csp, "unsafe-eval") && !strings.Contains(csp, "wasm-unsafe-eval") {
			t.Fatal(csp)
		}
	}
	resp, _ := srv.Client().Get(srv.URL + "/runtime-config.js")
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if !strings.Contains(string(b), `"strictLAN":true`) {
		t.Fatal(string(b))
	}
	for _, p := range []string{"/components/private.key", "/components/../private.key", "/components/%2e%2e/private.key", "/runtime-policy.test.cjs", "/models/", "/healthz?token=x"} {
		resp, err := srv.Client().Get(srv.URL + p)
		if err != nil {
			t.Fatal(err)
		}
		resp.Body.Close()
		if resp.StatusCode != 404 {
			t.Fatalf("%s: %d", p, resp.StatusCode)
		}
	}
	os.WriteFile(filepath.Join(mirror, "fixture.bin"), []byte("tampered"), 0644)
	resp, _ = srv.Client().Get(srv.URL + "/components/fixture.bin")
	resp.Body.Close()
	if resp.StatusCode != 409 {
		t.Fatal(resp.StatusCode)
	}
	req, _ := http.NewRequest("POST", srv.URL+"/healthz", nil)
	resp, _ = srv.Client().Do(req)
	resp.Body.Close()
	if resp.StatusCode != 405 {
		t.Fatal(resp.StatusCode)
	}
}
func TestRejectUnsafeOriginAndManifest(t *testing.T) {
	for _, raw := range []string{"https://example.com", "http://192.168.1.1", "https://8.8.8.8", "https://192.168.1.1/?x=1", "https://u:p@192.168.1.1", "https://[::ffff:8.8.8.8]"} {
		if _, err := localOrigin(raw); err == nil {
			t.Fatal(raw)
		}
	}
	web, mirror := fixture(t)
	os.Remove(filepath.Join(mirror, "fixture.bin"))
	os.Symlink("private.key", filepath.Join(mirror, "fixture.bin"))
	h, err := newHost(web, mirror, nil)
	if err != nil {
		t.Fatal(err)
	}
	defer h.Close()
	rr := httptest.NewRecorder()
	h.ServeHTTP(rr, httptest.NewRequest("GET", "/components/fixture.bin", nil))
	if rr.Code != 404 {
		t.Fatal(rr.Code)
	}
	os.WriteFile(filepath.Join(mirror, "manifest.json"), []byte(`{"schema":1,"components":[],"secret":"x"}`), 0644)
	if h, err := newHost(web, mirror, nil); err == nil {
		h.Close()
		t.Fatal("invalid manifest accepted")
	}
}
