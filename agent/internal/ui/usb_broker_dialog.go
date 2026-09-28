package ui

import (
	"fmt"

	"usbridge_agent/internal/usbpass"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/driver/desktop"
	"fyne.io/fyne/v2/widget"
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
		bodyText = "USB passthrough is powered by a separate, closed-source component (not open-source like the rest of this agent). It stays off until you enable it here. Once enabled, keyboard, mouse, and gamepad passthrough is free; other USB devices (drives, audio, tablets, etc.) require a Pro or Enterprise subscription."
	}

	showConfirmDialog(title, bodyText, onResult, parent)
}

// showUSBBrokerStatusDialog is what a tap on the USB Broker row shows once
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

// tappableBox is a simple container wrapper that intercepts taps and displays a pointer cursor.
type tappableBox struct {
	widget.BaseWidget
	content fyne.CanvasObject
	onTap   func()
}

func newTappableBox(content fyne.CanvasObject, onTap func()) *tappableBox {
	b := &tappableBox{content: content, onTap: onTap}
	b.ExtendBaseWidget(b)
	return b
}

func (b *tappableBox) CreateRenderer() fyne.WidgetRenderer {
	return widget.NewSimpleRenderer(b.content)
}

func (b *tappableBox) Tapped(*fyne.PointEvent) {
	if b.onTap != nil {
		b.onTap()
	}
}

func (b *tappableBox) TappedSecondary(*fyne.PointEvent) {}

func (b *tappableBox) Cursor() desktop.Cursor {
	return desktop.PointerCursor
}
