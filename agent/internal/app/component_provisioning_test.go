package app

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestComponentEntitlementRejectsInvalidVendorReply(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/desktop-license/refresh" {
			t.Errorf("unexpected request %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"free","license":"usbent1.invalid.signature","expires_in":3600}`))
	}))
	defer srv.Close()
	withBackendURL(t, srv.URL)
	a := newTestApp(t, "")
	if _, err := a.componentEntitlement(context.Background()); err == nil {
		t.Fatal("invalid signature accepted")
	}
	if a.cfg.EntitlementToken != "" {
		t.Fatal("invalid token was persisted")
	}
}

func TestDownloadRustShineRetainsOptInAfterOfflineFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	srv.Close()
	withBackendURL(t, srv.URL)
	a := newTestApp(t, "")
	if err := a.DownloadRustShine(nil); err == nil {
		t.Fatal("offline first setup unexpectedly succeeded")
	}
	if !a.cfg.StreamerConsent || a.cfg.PreferredBackend != "rustshine" {
		t.Fatal("setup intent lost; background retry cannot resume")
	}
	if a.cfg.USBBrokerConsentGiven() {
		t.Fatal("streamer selection must not consent to USB")
	}
	if a.rustshineStaged() {
		t.Fatal("a failed request must not create a staged executable")
	}
}

func TestProvisionRequestedComponentsDoesNothingWithoutConsent(t *testing.T) {
	a := newTestApp(t, "")
	if !a.provisionRequestedComponents(context.Background(), "") {
		t.Fatal("nothing requested should be ready")
	}
	if a.rustshineStaged() || a.cfg.USBBrokerConsentGiven() {
		t.Fatal("components enabled without consent")
	}
}

func TestExpiredEntitlementKeepsOptedInBackendIntent(t *testing.T) {
	a := newTestApp(t, "usbent1.invalid.signature")
	a.cfg.StreamerConsent = true
	a.cfg.PreferredBackend = "rustshine"
	a.downgradeToSunshine()
	if a.cfg.EntitlementToken != "" {
		t.Fatal("invalid token retained")
	}
	if a.cfg.PreferredBackend != "rustshine" {
		t.Fatal("explicit backend choice lost during temporary fallback")
	}
}
