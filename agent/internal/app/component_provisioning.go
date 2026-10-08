package app

import (
	"context"
	"fmt"
	"strings"

	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/hwid"
)

// componentEntitlement keeps the real vendor token intact. A free-tier token
// can provision components, but the closed programs still decide their features.
func (a *App) componentEntitlement(ctx context.Context) (string, error) {
	id, err := hwid.Get()
	if err != nil {
		return "", fmt.Errorf("hardware identity unavailable: %w", err)
	}
	token := strings.TrimSpace(a.cfg.EntitlementToken)
	if _, err := entitlement.VerifyForHardware(token, id); err == nil {
		return token, nil
	}
	res, err := entitlement.RefreshLicense(ctx, id)
	if err != nil {
		return "", fmt.Errorf("could not provision vendor entitlement (will retry in the background): %w", err)
	}
	if _, err := entitlement.VerifyForHardware(res.Token, id); err != nil {
		return "", fmt.Errorf("vendor entitlement verification failed: %w", err)
	}
	next := a.cfg
	next.EntitlementToken = res.Token
	if err := a.SaveConfig(next); err != nil {
		return "", err
	}
	a.refreshLocalEntitlementStatus()
	return res.Token, nil
}

// provisionRequestedComponents resumes interrupted setup only after explicit
// component consent. It never opts into USB or starts sharing a device.
func (a *App) provisionRequestedComponents(ctx context.Context, token string) bool {
	a.componentMu.Lock()
	defer a.componentMu.Unlock()
	ready := true
	requested := a.cfg.PreferredBackend == "rustshine"
	if (a.cfg.StreamerConsent && requested) || a.rustshineStaged() {
		a.ensureRustShineFresh(ctx, token)
		if requested && !a.rustshineStaged() {
			ready = false
		}
		if requested && a.rustshineStaged() && a.currentStreamKind() != "rustshine" {
			if err := a.SetStreamBackend("rustshine"); err != nil {
				a.setEntError(fmt.Sprintf("streamer startup failed: %v", err))
				ready = false
			}
		}
	}
	if a.usbBroker != nil && a.cfg.USBBrokerConsentGiven() && !a.usbBroker.Staged() {
		if err := a.stageAndStartUSBBroker(ctx, token, nil); err != nil {
			a.setEntError(fmt.Sprintf("USB broker setup failed: %v", err))
			ready = false
		}
	}
	return ready
}
