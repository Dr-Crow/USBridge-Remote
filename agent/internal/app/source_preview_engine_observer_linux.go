//go:build linux && source_preview_engine_acceptance

package app

import (
	"net"
	"strconv"

	"usbridge_agent/internal/localruntime"
	"usbridge_agent/internal/netpolicy"
)

// SourcePreviewEngineMetadata exposes only bounded non-sensitive facts to the
// read-only CI observer. It cannot configure the engine or close its sockets.
// In particular the USBPass listener remains the real App.New-created listener;
// the acceptance driver must prove actual process exit releases it.
func (a *App) SourcePreviewEngineMetadata() map[string]any {
	host, port, err := net.SplitHostPort(a.usbPassBridgeAddr)
	n, _ := strconv.Atoi(port)
	return map[string]any{
		"app_new_completed": a.apiServer != nil && a.input != nil && a.perms != nil,
		"strict_lan":        netpolicy.Strict(), "runtime_local": netpolicy.RuntimeLocal(),
		"local_runtime_enabled":  localruntime.Enabled(),
		"streamer_consent":       a.cfg.StreamerConsent,
		"usb_consent":            a.cfg.USBBrokerConsentGiven(),
		"stock_streamer_running": a.StreamerRunning(), "stock_session_active": a.SessionActive(),
		"account_logged_in": a.AccountStatus().LoggedIn,
		"tailscale_enabled": a.cfg.TailscaleEnabled, "tls_enabled": a.cfg.TLSEnabledOK(),
		"clipboard_enabled": a.cfg.ClipboardSyncEnabled,
		"usbpass_loopback":  err == nil && host == "127.0.0.1" && n > 0,
		"usbpass_port":      n,
	}
}
