package streamhost

import (
	"os"
	"path/filepath"
	"testing"
	"usbridge_agent/internal/netpolicy"
)

func TestStrictLANDoesNotLaunchUnverifiedLegacyOrEnvironmentPaths(t *testing.T) {
	t.Setenv(netpolicy.Environment, "1")
	dir := t.TempDir()
	p := filepath.Join(dir, "unverified")
	if err := os.WriteFile(p, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USBRIDGE_PUNKTFUNK_HOST", p)
	prior, _ := sunshineStageBinary.Load().(string)
	t.Cleanup(func() { SetSunshineStageBinary(prior) })
	SetSunshineStageBinary(p)
	for _, b := range []Backend{NewRustshine(dir, dir, ""), NewSunshine(dir, dir, ""), NewPunktfunk(dir, dir, "")} {
		if got := b.BinaryPath(); got != "" {
			t.Fatalf("unverified path accepted: %s", got)
		}
	}
}
