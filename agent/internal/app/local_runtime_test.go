package app

import (
	"context"
	"os"
	"path/filepath"
	"testing"

	"usbridge_agent/internal/config"
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

func TestLocalRuntimePreferencePersistsWithoutChangingLiveModeOrConsent(t *testing.T) {
	for _, active := range []string{"", "1"} {
		t.Run("active="+active, func(t *testing.T) {
			t.Setenv(localruntime.Environment, active)
			a := newTestApp(t, "vendor-token-left-untouched")
			for _, enabled := range []bool{true, false} {
				if err := a.SetLocalRuntimeEnabled(enabled); err != nil {
					t.Fatal(err)
				}
				saved, err := config.Load(a.cfgPath)
				if err != nil {
					t.Fatal(err)
				}
				if saved.LocalRuntimeEnabled != enabled {
					t.Fatal("preference not persisted")
				}
				if localruntime.Enabled() != (active == "1") {
					t.Fatal("live engine changed without restart")
				}
				if saved.EntitlementToken != "vendor-token-left-untouched" || saved.StreamerConsent || saved.USBBrokerConsentGiven() {
					t.Fatal("runtime preference changed credentials or consent")
				}
			}
		})
	}
}

func TestLocalRuntimePreferenceFailedSaveRetainsMode(t *testing.T) {
	t.Setenv(localruntime.Environment, "")
	a := newTestApp(t, "")
	// A file in the parent path makes persistence fail on every platform.
	block := filepath.Join(t.TempDir(), "file")
	if err := os.WriteFile(block, []byte("blocked"), 0600); err != nil {
		t.Fatal(err)
	}
	a.cfgPath = filepath.Join(block, "config.yaml")
	if err := a.SetLocalRuntimeEnabled(true); err == nil {
		t.Fatal("save unexpectedly succeeded")
	}
	if a.cfg.LocalRuntimeEnabled || localruntime.Enabled() {
		t.Fatal("failed save changed mode")
	}
}

func TestServiceRuntimeGuardHonorsSavedAndOverrideModes(t *testing.T) {
	for _, tc := range []struct {
		env           string
		saved, reject bool
	}{
		{"", false, false}, {"", true, true}, {"1", false, true}, {"1", true, true},
	} {
		t.Setenv(localruntime.Environment, tc.env)
		if rejected := validateServiceRuntime(tc.saved) != nil; rejected != tc.reject {
			t.Fatalf("env=%q saved=%t: rejected=%t", tc.env, tc.saved, rejected)
		}
	}
}
