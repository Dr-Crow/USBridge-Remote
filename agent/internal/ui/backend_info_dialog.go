package ui

import "fyne.io/fyne/v2"

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
	showInfoDialog(title, message, parent)
}
