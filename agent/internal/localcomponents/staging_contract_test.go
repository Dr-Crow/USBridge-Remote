package localcomponents

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The selected package is input to installation, never an alternate executable
// location. The preview supervisor depends on this boundary immediately before
// launch, including when the source package remains valid after staging fails.
func TestPreparedComponentUsesIndependentStateCopy(t *testing.T) {
	dir, raw := fixture(t, "staging-contract")
	state := filepath.Join(t.TempDir(), "source-preview")
	r, err := Resolve(context.Background(), Options{StateDir: state, Directory: dir, ManifestSHA256: digest(raw)}, "streamer")
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(state, "local-components", "streamer", "bin", "streamer")
	if r.Binary != want || PreparedPath(state, "streamer") != want {
		t.Fatalf("component did not use its state installation: %q", r.Binary)
	}
	sourcePath := filepath.Join(dir, "bin", "streamer")
	sourceInfo, err := os.Stat(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	stagedInfo, err := os.Stat(r.Binary)
	if err != nil {
		t.Fatal(err)
	}
	if os.SameFile(sourceInfo, stagedInfo) {
		t.Fatal("staged executable aliases the selected package")
	}
	original, err := os.ReadFile(sourcePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(sourcePath, []byte("changed source after installation"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPrepared(r.Binary); err != nil {
		t.Fatalf("independent installed copy no longer verifies: %v", err)
	}
	if err := VerifyPrepared(sourcePath); err == nil {
		t.Fatal("uninstalled source path accepted for execution")
	}
	// A valid original package must not silently repair or replace a failed
	// just-before-launch verification of the selected installation.
	if err := os.WriteFile(sourcePath, original, 0600); err != nil {
		t.Fatal(err)
	}
	corrupt := []byte("changed staged executable")
	if err := os.WriteFile(r.Binary, corrupt, 0600); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPrepared(r.Binary); err == nil {
		t.Fatal("tampered staged executable accepted via valid source fallback")
	}
	if got, err := os.ReadFile(r.Binary); err != nil || string(got) != string(corrupt) {
		t.Fatal("verification unexpectedly repaired the failed installation")
	}
	if PreparedPath(state, "streamer") != "" || IsPreparedPath(r.Binary) {
		t.Fatal("failed verification retained a launchable prepared path")
	}
}

func TestPreparedComponentRejectsSymlinkToValidSource(t *testing.T) {
	dir, raw := fixture(t, "staging-symlink")
	state := t.TempDir()
	r, err := Resolve(context.Background(), Options{StateDir: state, Directory: dir, ManifestSHA256: digest(raw)}, "streamer")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(r.Binary); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(dir, "bin", "streamer"), r.Binary); err != nil {
		t.Fatal(err)
	}
	if err := VerifyPrepared(r.Binary); err == nil {
		t.Fatal("staged symlink accepted even though target has correct bytes")
	}
	if PreparedPath(state, "streamer") != "" {
		t.Fatal("symlink rejection retained the prepared path")
	}
}
