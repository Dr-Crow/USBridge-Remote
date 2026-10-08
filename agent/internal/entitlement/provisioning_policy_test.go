package entitlement

import (
	"context"
	"testing"
	"usbridge_agent/internal/netpolicy"
)

func TestRuntimeLocalOnlyPermitsScopedComponentRoutes(t *testing.T) {
	t.Setenv(netpolicy.Environment, "")
	netpolicy.Configure(false)
	netpolicy.ConfigureRuntimeLocal(true)
	t.Cleanup(func() { netpolicy.Configure(false); netpolicy.ConfigureRuntimeLocal(false) })
	ordinary := context.Background()
	setup := netpolicy.WithPublicProvisioning(ordinary)
	for _, path := range []string{"/v1/desktop-license/refresh", "/v1/download/rustshine?platform=windows-x86_64", "/v1/download/usb-broker?platform=linux-x86_64"} {
		if _, err := newRequest(ordinary, "POST", path, nil); err == nil {
			t.Errorf("unscoped request allowed: %s", path)
		}
		if _, err := newRequest(setup, "POST", path, nil); err != nil {
			t.Errorf("setup blocked: %s: %v", path, err)
		}
	}
	for _, path := range []string{"/v1/webrtc/turn-credentials", "/v1/desktop-billing/checkout", "/v1/download/unknown", "/v1/download/rustshine/other"} {
		if _, err := newRequest(setup, "POST", path, nil); err == nil {
			t.Errorf("setup enabled unrelated endpoint: %s", path)
		}
	}
	netpolicy.Configure(true)
	if _, err := newRequest(setup, "POST", "/v1/desktop-license/refresh", nil); err == nil {
		t.Fatal("strict offline bypassed")
	}
}
