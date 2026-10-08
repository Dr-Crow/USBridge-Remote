package entitlement

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"usbridge_agent/internal/netpolicy"
)

func TestStrictLANRejectsEntitlementAndRelayBeforeNetwork(t *testing.T) {
	t.Setenv(netpolicy.Environment, "1")
	// Even a cached token may not authorize network access under this policy.
	if req, err := newRequest(context.Background(), http.MethodPost, "/v1/desktop-license/refresh", nil); req != nil || !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("request=%v error=%v", req, err)
	}
	if _, err := RefreshLicense(context.Background(), "local-hardware-id"); !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("refresh: %v", err)
	}
	if _, err := DialSignalRelay(context.Background(), "local-hardware-id"); !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("relay: %v", err)
	}
	if _, err := downloadArchive(context.Background(), "https://example.invalid/component", "", nil); !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("download: %v", err)
	}
}
