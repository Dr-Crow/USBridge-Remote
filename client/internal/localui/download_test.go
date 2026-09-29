//go:build !(js && wasm)

package localui

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// withTestModels temporarily swaps the package-level Models/modelURL to
// point at an httptest server instead of raw.githubusercontent.com, so
// these tests never touch the network. Restores the originals on return.
func withTestModels(t *testing.T, content map[string][]byte) (models []Model, srv *httptest.Server) {
	t.Helper()
	mux := http.NewServeMux()
	for name, data := range content {
		data := data
		mux.HandleFunc("/"+name, func(w http.ResponseWriter, r *http.Request) {
			w.Write(data)
		})
	}
	srv = httptest.NewServer(mux)
	t.Cleanup(srv.Close)

	for name, data := range content {
		sum := sha256.Sum256(data)
		models = append(models, Model{Filename: name, SHA256: hex.EncodeToString(sum[:]), Size: int64(len(data))})
	}
	return models, srv
}

// testDownloadModels mirrors DownloadModels but against the given models
// slice and base URL, so tests don't need to touch the real package-level
// Models/modelURL (which point at raw.githubusercontent.com).
func testDownloadModels(ctx context.Context, dir, baseURL string, models []Model, onProgress ProgressFunc) error {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	client := &http.Client{}
	for i, m := range models {
		if fi, err := os.Stat(filepath.Join(dir, m.Filename)); err == nil && fi.Size() == m.Size {
			continue
		}
		mURL := m
		orig := modelURLFunc
		modelURLFunc = func(filename string) string { return baseURL + "/" + filename }
		err := downloadOneModel(ctx, client, dir, mURL, i, len(models), onProgress)
		modelURLFunc = orig
		if err != nil {
			return err
		}
	}
	return nil
}

func TestDownloadModelsHappyPath(t *testing.T) {
	content := map[string][]byte{
		"a.onnx": []byte("hello world model a"),
		"b.onnx": []byte("model b content, a bit longer than a"),
	}
	models, srv := withTestModels(t, content)
	dir := t.TempDir()

	var lastProgress DownloadProgress
	err := testDownloadModels(context.Background(), dir, srv.URL, models, func(p DownloadProgress) { lastProgress = p })
	if err != nil {
		t.Fatalf("DownloadModels failed: %v", err)
	}
	if lastProgress.File == "" {
		t.Error("onProgress was never called")
	}

	for name, want := range content {
		got, err := os.ReadFile(filepath.Join(dir, name))
		if err != nil {
			t.Fatalf("reading %s: %v", name, err)
		}
		if string(got) != string(want) {
			t.Errorf("%s content = %q, want %q", name, got, want)
		}
	}

	// No leftover .part-* temp files.
	entries, _ := os.ReadDir(dir)
	for _, e := range entries {
		if strings.Contains(e.Name(), ".part-") {
			t.Errorf("leftover temp file: %s", e.Name())
		}
	}
}

func TestDownloadModelsHashMismatchLeavesNoFile(t *testing.T) {
	dir := t.TempDir()
	badContent := []byte("this is NOT what the hash says")
	mux := http.NewServeMux()
	mux.HandleFunc("/bad.onnx", func(w http.ResponseWriter, r *http.Request) { w.Write(badContent) })
	srv := httptest.NewServer(mux)
	defer srv.Close()

	m := Model{Filename: "bad.onnx", SHA256: strings.Repeat("0", 64), Size: int64(len(badContent))}
	orig := modelURLFunc
	modelURLFunc = func(filename string) string { return srv.URL + "/" + filename }
	defer func() { modelURLFunc = orig }()

	err := downloadOneModel(context.Background(), &http.Client{}, dir, m, 0, 1, nil)
	if err == nil {
		t.Fatal("expected a sha256 mismatch error, got nil")
	}
	if !strings.Contains(err.Error(), "sha256 mismatch") {
		t.Errorf("error = %v, want a sha256 mismatch error", err)
	}
	if _, statErr := os.Stat(filepath.Join(dir, "bad.onnx")); !os.IsNotExist(statErr) {
		t.Error("a file was left behind at the real path despite the hash mismatch -- ModelsPresent would wrongly report this model as installed")
	}
	entries, _ := os.ReadDir(dir)
	if len(entries) != 0 {
		t.Errorf("expected the temp file to be cleaned up too, found: %v", entries)
	}
}

func TestModelsPresent(t *testing.T) {
	dir := t.TempDir()
	models := []Model{
		{Filename: "x.onnx", Size: 5},
		{Filename: "y.onnx", Size: 3},
	}
	if modelsPresentIn(dir, models) {
		t.Error("ModelsPresent should be false for an empty dir")
	}
	os.WriteFile(filepath.Join(dir, "x.onnx"), []byte("12345"), 0o644)
	if modelsPresentIn(dir, models) {
		t.Error("ModelsPresent should still be false with only one of two files present")
	}
	os.WriteFile(filepath.Join(dir, "y.onnx"), []byte("12"), 0o644) // wrong size (2, not 3)
	if modelsPresentIn(dir, models) {
		t.Error("ModelsPresent should be false when a file exists but is the wrong size (e.g. a truncated download)")
	}
	os.WriteFile(filepath.Join(dir, "y.onnx"), []byte("123"), 0o644)
	if !modelsPresentIn(dir, models) {
		t.Error("ModelsPresent should be true once every file exists at its expected size")
	}
}
