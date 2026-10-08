package app

import (
	"context"
	"fmt"
	"strings"
	"time"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/hwid"
	"usbridge_agent/internal/netpolicy"
)

// componentEntitlement keeps the real vendor token intact. A free-tier token
// can provision components, but the closed programs still decide their features.
func (a *App) componentEntitlement(ctx context.Context) (string, error) {
	ctx = netpolicy.WithPublicProvisioning(ctx)
	if err := netpolicy.RequireProvisioning(ctx, "vendor component entitlement"); err != nil {
		return "", err
	}
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

// requestedProvisioningWatchdog retries only components already selected by the
// user and missing from disk. Once staged it performs no vendor polling, refresh
// or update checks. Strict offline mode never starts this loop.
func (a *App) requestedProvisioningWatchdog(ctx context.Context) {
	if !netpolicy.RuntimeLocal() || netpolicy.Strict() {
		return
	}
	for {
		if ctx.Err() != nil {
			return
		}
		saved, readErr := config.Load(a.cfgPath)
		needsStreamer := readErr == nil && saved.StreamerConsent && saved.PreferredBackend == "rustshine" && !a.rustshineStaged()
		needsBroker := readErr == nil && saved.USBBrokerConsentGiven() && a.usbBroker != nil && !a.usbBroker.Staged()
		if needsStreamer || needsBroker {
			setupCtx := netpolicy.WithPublicProvisioning(ctx)
			token, err := a.componentEntitlement(setupCtx)
			if err != nil {
				a.setEntError(err.Error())
			} else {
				a.componentMu.Lock()
				if needsStreamer {
					if err := a.stageRustShine(setupCtx, token, nil); err != nil {
						a.setEntError(err.Error())
					} else if err := a.SetStreamBackend("rustshine"); err != nil {
						a.setEntError(err.Error())
					}
				}
				if needsBroker {
					if err := a.stageAndStartUSBBroker(setupCtx, token, nil); err != nil {
						a.setEntError(err.Error())
					}
				}
				a.componentMu.Unlock()
			}
		}
		timer := time.NewTimer(entitlementRetryInterval)
		select {
		case <-ctx.Done():
			timer.Stop()
			return
		case <-timer.C:
		}
	}
}
