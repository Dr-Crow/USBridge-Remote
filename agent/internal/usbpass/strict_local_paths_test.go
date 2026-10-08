package usbpass

import (
	"os"
	"path/filepath"
	"testing"
	"usbridge_agent/internal/netpolicy"
)

func TestStrictLANIgnoresUnverifiedBrokerOverride(t *testing.T) {
	t.Setenv(netpolicy.Environment, "1")
	p := filepath.Join(t.TempDir(), "unverified")
	if err := os.WriteFile(p, []byte("fixture"), 0755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("USBRIDGE_USB_BROKER", p)
	s := &Service{stateDir: t.TempDir()}
	if s.resolveBroker() != "" {
		t.Fatal("unverified override accepted")
	}
}
