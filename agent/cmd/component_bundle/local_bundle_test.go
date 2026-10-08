package main

import (
	"archive/zip"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
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
