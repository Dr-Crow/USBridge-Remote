package localruntime

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
)

// Explicit stock-binary parser probe. The --creds mode writes synthetic admin
// credentials into a temporary directory and exits without starting the server.
// This establishes CLI empty-value acceptance only, not ICE/network behavior.
func TestPinnedStreamerEmptyICECLI(t *testing.T) {
	source := os.Getenv("USBRIDGE_LAB_STREAMER_BINARY")
	if source == "" {
		t.Skip("explicit pinned streamer path required")
	}
	data, err := os.ReadFile(source)
	if err != nil {
		t.Fatal(err)
	}
	before := sha256.Sum256(data)
	expected := pinned[runtime.GOOS+"/"+runtime.GOARCH+"/rustshine"]
	if expected == "" || hex.EncodeToString(before[:]) != expected {
		t.Fatal("refusing unrecognized streamer")
	}
	dir := t.TempDir()
	credentials := filepath.Join(dir, "parser-probe-credentials.json")
	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, source, "--webrtc-ice-servers=", "--credentials-path", credentials, "--creds", "parser-probe", "synthetic-test-only")
	cmd.Dir = dir
	// Do not inherit unrelated ICE/TURN values from the CI environment.
	for _, e := range os.Environ() {
		if strings.HasPrefix(e, "USBRIDGE_STREAMER_WEBRTC_ICE_SERVERS=") || strings.HasPrefix(e, "USBRIDGE_STREAMER_TURN_CREDENTIALS_FILE=") {
			continue
		}
		cmd.Env = append(cmd.Env, e)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("empty ICE CLI probe failed: %v; output=%s", err, out)
	}
	info, err := os.Stat(credentials)
	if err != nil || info.Size() == 0 {
		t.Fatal("one-shot credentials path did not execute")
	}
	after, err := os.ReadFile(source)
	if err != nil || sha256.Sum256(after) != before {
		t.Fatal("stock executable changed")
	}
	t.Log("Pinned stock streamer accepted --webrtc-ice-servers= and completed the one-shot credentials command; no streaming or network-egress acceptance claimed")
}
