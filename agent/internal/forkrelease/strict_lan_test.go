package forkrelease

import (
	"context"
	"errors"
	"testing"
	"usbridge_agent/internal/netpolicy"
)

func TestStrictLANRejectsPublicReleaseBeforeNetwork(t *testing.T) {
	t.Setenv(netpolicy.Environment, "1")
	if _, err := fetch(context.Background(), "https://example.invalid/manifest"); !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("metadata: %v", err)
	}
	if _, err := download(context.Background(), "https://example.invalid/component", "", nil); !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("artifact: %v", err)
	}
}
