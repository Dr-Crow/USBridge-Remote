package ui

import (
	"fmt"

	"usbridge_agent/internal/usbpass"

	"fyne.io/fyne/v2"
)

// showUSBBrokerDialog displays the branded confirmation modal when clicking
// on USB Broker, styled identically to the Autostart at Boot info dialog
// with Yes/No actions in the footer.
func (w *Window) showUSBBrokerDialog(parent fyne.Window, onResult func(bool)) {
	showUSBBrokerDialog(parent, onResult)
}

// showUSBBrokerDialog displays the modal popup with the application design.
func showUSBBrokerDialog(parent fyne.Window, onResult func(bool)) {
	title := loc().USBBrokerConsentTitle
	if title == "" {
		title = "Enable USB passthrough?"
	}

	bodyText := loc().USBBrokerConsentBody
	if bodyText == "" {
		bodyText = "USB passthrough is powered by a separate, closed-source component (not open-source like the rest of this agent). It stays off until you enable it here. The separate broker decides which devices it accepts. Enabling it does not start sharing devices."
	}

	showConfirmDialog(title, bodyText, onResult, parent)
}

// showUSBBrokerStatusDialog is what the USB Broker Info button shows once
// consent is given: running + port, or not running + the broker's own last
// error line (usbpass.Status.BrokerLastExit).
func showUSBBrokerStatusDialog(parent fyne.Window, st usbpass.Status) {
	var body string
	if st.BrokerAlive {
		body = fmt.Sprintf(loc().USBBrokerRunningOnPort, st.ListenPort) + "\n\n" + loc().USBBrokerFreeTierNote
	} else {
		body = loc().USBBrokerNotRunning
		reason := st.BrokerLastExit
		if reason == "" {
			reason = st.BrokerError
		}
		if reason != "" {
			body += "\n\n" + loc().USBBrokerLastError + "\n" + reason
		}
	}
	showInfoDialog(loc().USBBroker, body, parent)
}
