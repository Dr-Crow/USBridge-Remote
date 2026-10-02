//go:build !windows

package streamhost

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// A patched host answers the probe; a stock one fails it. The answer is
// re-asked when the binary is replaced.
func TestPunktfunkHasBridge(t *testing.T) {
	bin := filepath.Join(t.TempDir(), "punktfunk-host")
	write := func(script string, mod time.Time) {
		t.Helper()
		if err := os.WriteFile(bin, []byte("#!/bin/sh\n"+script+"\n"), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.Chtimes(bin, mod, mod); err != nil {
			t.Fatal(err)
		}
	}
	t0 := time.Now().Add(-time.Hour).Truncate(time.Second)

	write(`echo "unknown command '$1' (try --help)" >&2; exit 1`, t0)
	if punktfunkHasBridge(bin) {
		t.Error("a stock punktfunk-host must not report the bridge")
	}

	write(`[ "$1" = usbridge-bridge ] && echo "usbridge-bridge 1"`, t0.Add(time.Minute))
	if !punktfunkHasBridge(bin) {
		t.Error("a patched punktfunk-host must report the bridge")
	}
	b := &punktfunkBackend{}
	t.Setenv("PATH", t.TempDir())
	t.Setenv(punktfunkBinEnv, bin)
	if !b.RawHIDSupported() {
		t.Error("RawHIDSupported should follow the probe")
	}

	if punktfunkHasBridge("") || punktfunkHasBridge(filepath.Join(t.TempDir(), "missing")) {
		t.Error("no binary, no bridge")
	}
}
