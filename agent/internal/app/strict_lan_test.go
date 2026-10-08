package app

import (
	"context"
	"testing"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/netpolicy"
)

func TestStrictLANExpiredVendorTokenDoesNotDowngradeBackendIntent(t *testing.T) {
	t.Setenv(netpolicy.Environment, "1")
	a := &App{cfg: config.Config{PreferredBackend: "rustshine", EntitlementToken: "expired-invalid-vendor-token"}}
	if !a.recheckEntitlement(context.Background()) {
		t.Fatal("strict recheck requested a vendor retry")
	}
	if a.cfg.PreferredBackend != "rustshine" || a.cfg.EntitlementToken != "expired-invalid-vendor-token" {
		t.Fatal("vendor expiry changed local backend intent/token")
	}
}
