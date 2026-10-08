package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestBundlePlatformMatrix(t *testing.T) {
	for target, want := range map[string]string{"windows/amd64": "windows-x86_64", "linux/amd64": "linux-x86_64", "darwin/arm64": "macos-arm64"} {
		_, _, got, err := bundlePlatform(target)
		if err != nil || got != want {
			t.Fatal(target, got, err)
		}
	}
	for _, target := range []string{"darwin/amd64", "linux/arm64", "windows/arm64", "../windows/amd64"} {
		if _, _, _, err := bundlePlatform(target); err == nil {
			t.Fatal("unsupported target accepted", target)
		}
	}
}
func TestBundleRejectsUnpinnedAndUnsafeArchiveMembers(t *testing.T) {
	for _, name := range []string{"dir/usbridge-streamer.exe", "../usbridge-streamer.exe", "/usbridge-streamer.exe"} {
		out := t.TempDir()
		asset := "input.zip"
		f, err := os.Create(filepath.Join(out, asset))
		if err != nil {
			t.Fatal(err)
		}
		z := zip.NewWriter(f)
		w, err := z.Create(name)
		if err != nil {
			t.Fatal(err)
		}
		w.Write([]byte("not an audited executable"))
		z.Close()
		f.Close()
		data, _ := os.ReadFile(filepath.Join(out, asset))
		sum := sha256.Sum256(data)
		err = writeLocalBundle(out, []bundleInput{{Name: "rustshine", Version: "usbridge-streamer-v0.3.131", Asset: asset, SHA256: hex.EncodeToString(sum[:])}}, "windows", "amd64")
		if err == nil {
			t.Fatal("unsafe/unpinned member accepted", name)
		}
		if _, err := os.Stat(filepath.Join(out, "bundle.json")); !os.IsNotExist(err) {
			t.Fatal("failed bundle was advertised")
		}
	}
}

func TestBundleWritesReplaceCachedFilesWithoutFollowingLinks(t *testing.T) {
	dir := t.TempDir()
	root, err := os.OpenRoot(dir)
	if err != nil {
		t.Fatal(err)
	}
	defer root.Close()
	if err := os.WriteFile(filepath.Join(dir, "cached"), []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := writeBundleFile(root, "cached", []byte("new"), 0755); err != nil {
		t.Fatal(err)
	}
	got, err := root.ReadFile("cached")
	if err != nil || string(got) != "new" {
		t.Fatal("replacement failed", err)
	}
	st, err := root.Stat("cached")
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" && st.Mode().Perm() != 0755 {
		t.Fatalf("cached mode not corrected: %v", st.Mode())
	}
	outside := filepath.Join(t.TempDir(), "target")
	if err := os.WriteFile(outside, []byte("untouched"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := root.Symlink(outside, "link"); err != nil {
		t.Skipf("symlink unavailable: %v", err)
	}
	if err := writeBundleFile(root, "link", []byte("replacement"), 0644); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(outside)
	if err != nil || string(raw) != "untouched" {
		t.Fatal("external symlink target changed")
	}
	if err := root.Symlink(filepath.Dir(outside), "external-dir"); err != nil {
		t.Fatal(err)
	}
	if err := writeBundleFile(root, "external-dir/escape", []byte("no"), 0644); err == nil {
		t.Fatal("directory symlink escaped output root")
	}
}
