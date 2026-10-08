package ui

import (
	"fyne.io/fyne/v2"
	"usbridge_agent/internal/localruntime"
)

func (w *Window) showBackendInfoDialog(parent fyne.Window, key string) {
	if parent == nil {
		return
	}
	title := "USBridge streamer"
	message := "Selecting this backend requests a genuine vendor entitlement and downloads the separate streamer after your consent. Interrupted setup retries in the background. The downloaded streamer still enforces its own capabilities and entitlement. USB passthrough has a separate opt-in; the broker may refuse a device or feature."
	switch key {
	case protocolOpensource:
		title = "Sunshine"
		message = "Open-source streaming backend. 4:4:4 availability depends on encoder and client support, not the agent's subscription tier."
	case protocolPunktfunk:
		title = "Punktfunk"
		message = "Alternative open-source streaming backend. Available features depend on the installed backend and connected client."
	}
	if localruntime.Enabled() && key == protocolFree {
		message = "EXPERIMENTAL local runtime mode is active. The agent runs rekeyed copies of the pinned v0.3.131 binaries with locally signed test claims. Downloaded vendor originals stay unchanged. This is not an unmodified-stock unlock, vendor subscription, or fully offline networking mode. Streaming/USB hardware acceptance is still required."
	}
	showInfoDialog(title, message, parent)
}
