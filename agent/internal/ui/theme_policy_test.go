package ui

import (
	"testing"

	"fyne.io/fyne/v2/test"
	"usbridge_agent/internal/account"
	"usbridge_agent/internal/entitlement"
)

type themePolicyProvider struct {
	TokenProvider
	status entitlement.Status
}

func (p *themePolicyProvider) EntitlementStatus() entitlement.Status { return p.status }
func (p *themePolicyProvider) AccountStatus() account.Status         { return account.Status{} }

func TestProThemeIsCosmeticAndSurvivesEntitlementChanges(t *testing.T) {
	app := test.NewApp()
	defer app.Quit()
	chromeMu.Lock()
	oldNow, oldPin, oldProto := chromeNow, chromePin, chromeProtocol
	chromeMu.Unlock()
	t.Cleanup(func() {
		chromeMu.Lock()
		chromeNow, chromePin, chromeProtocol = oldNow, oldPin, oldProto
		chromeMu.Unlock()
	})
	provider := &themePolicyProvider{}
	w := &Window{app: app, token: provider}
	for _, tier := range []string{"free", "pro", "enterprise", ""} {
		provider.status = entitlement.Status{Tier: tier}
		w.saveChromePin(protocolPro)
		if got := app.Preferences().String(chromeThemePrefKey); got != "pro" {
			t.Fatalf("tier %q: saved preference %q", tier, got)
		}
		setChromePin("")
		w.loadChromePin()
		w.refreshTierBadge(provider.status)
		if chromePinned() != protocolPro || currentChrome().Kind != protocolPro {
			t.Fatalf("tier %q changed the user's cosmetic theme", tier)
		}
	}
	// Default still follows the actual backend/tier rather than pretending Pro.
	w.saveChromePin("")
	w.refreshTierBadge(entitlement.Status{Tier: "free", ActiveBackend: "rustshine"})
	if chromePinned() != "" || currentChrome().Kind != protocolFree {
		t.Fatal("default theme should still follow the actual free protocol")
	}
}
