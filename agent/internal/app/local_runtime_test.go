package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/localruntime"
)

func TestLocalRuntimeStagedSelectionDoesNotReplaceVendorToken(t *testing.T) {
	t.Setenv(localruntime.Environment, "1")
	a := newTestApp(t, "vendor-token-left-untouched")
	p := entitlement.StagePath(a.cfg.StateDir)
	if err := os.MkdirAll(filepath.Dir(p), 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("staging-presence-only; not executed"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := a.DownloadRustShine(nil); err != nil {
		t.Fatal(err)
	}
	if a.cfg.EntitlementToken != "vendor-token-left-untouched" {
		t.Fatal("local mode replaced vendor credential")
	}
	if a.cfg.USBBrokerConsentGiven() {
		t.Fatal("local mode enabled USB without consent")
	}
	a.setStreamKind("rustshine")
	if !a.recheckEntitlement(context.Background()) {
		t.Fatal("staged local runtime unexpectedly needed vendor refresh")
	}
	if a.currentStreamKind() != "rustshine" {
		t.Fatal("local runtime downgraded on vendor expiry")
	}
}
