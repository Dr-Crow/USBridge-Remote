package forkrelease

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

func TestSafeRelPath(t *testing.T) {
	for name, ok := range map[string]bool{
		"sunshine.exe":     true,
		"assets/web/x.js":  true,
		"../evil":          false,
		"a/../../evil":     false,
		"/etc/passwd":      false,
		`C:\Windows\x.dll`: false,
	} {
		if _, got := safeRelPath(name); got != ok {
			t.Errorf("safeRelPath(%q) = %v, want %v", name, got, ok)
		}
	}
}

// A second download replaces the build but keeps a config\ Sunshine wrote
// next to itself.
func TestLiveSunshineDownload(t *testing.T) {
	if os.Getenv("USBRIDGE_LIVE_FORKRELEASE") == "" {
		t.Skip("set USBRIDGE_LIVE_FORKRELEASE=1 to download from GitHub")
	}
	if SunshineAssetName() == "" {
		t.Skip("Sunshine is bundled with the agent on this platform")
	}
	state := t.TempDir()
	for i := 0; i < 2; i++ {
		prep, err := PrepareSunshine(context.Background(), state, nil)
		if err != nil {
			t.Fatal(err)
		}
		if err := prep.Commit(); err != nil {
			t.Fatal(err)
		}
		if !SunshineStaged(state) || SunshineStagedVersion(state) != prep.Version {
			t.Fatalf("staged=%v version=%q, want %q", SunshineStaged(state), SunshineStagedVersion(state), prep.Version)
		}
		if _, err := os.Stat(filepath.Join(SunshineDir(state), "assets")); err != nil {
			t.Fatalf("no assets next to sunshine.exe: %v", err)
		}
		cfg := filepath.Join(SunshineDir(state), "config", "keep.txt")
		if i == 0 {
			if err := os.MkdirAll(filepath.Dir(cfg), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(cfg, []byte("x"), 0o644); err != nil {
				t.Fatal(err)
			}
		} else if _, err := os.Stat(cfg); err != nil {
			t.Fatalf("config lost on update: %v", err)
		}
	}
	for _, leftover := range []string{".next", ".old"} {
		if _, err := os.Stat(SunshineDir(state) + leftover); err == nil {
			t.Errorf("%s left behind", leftover)
		}
	}
}
