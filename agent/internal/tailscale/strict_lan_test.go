package tailscale

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"
	"usbridge_agent/internal/netpolicy"
)

func TestStrictLANNeverStartsTSNetOrCreatesItsState(t *testing.T) {
	t.Setenv(netpolicy.Environment, "1")
	dir := filepath.Join(t.TempDir(), "tsnet-state")
	s := New(dir)
	t.Cleanup(func() { _ = s.Close() })
	if server, err := s.Server(); server != nil || !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("server=%v error=%v", server, err)
	}
	status, err := s.Status(context.Background())
	if err != nil || status.Running || status.Backend != "Disabled by strict-LAN policy" {
		t.Fatalf("status=%+v error=%v", status, err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("tsnet state created: %v", err)
	}
	if s.server != nil {
		t.Fatal("tsnet server constructed")
	}
}
