package ui

import (
	"net/url"
	"runtime"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/ui/design"
	"usbridge_agent/internal/usbpass"
)

// usbipWin2ReleasesURL is where the Windows USB Passthrough "Download"
// button sends the user: usbip-win2 ships its own signed installer.
const usbipWin2ReleasesURL = "https://github.com/vadimgrn/usbip-win2/releases/latest"

// usbridgeCreativeProURL is the USBridge marketing site's creative/pro
// section -- stylus pressure/tilt, color accuracy -- linked from the macOS
// dongle info dialog below instead of just explaining the technical cause.
const usbridgeCreativeProURL = "https://www.usbridge.io/#creative-pro-graphics"

// usbPermGranted is the USB Passthrough chip's tick: on Linux the one-time
// polkit attach grant, on Windows whether usbip-win2's drivers are
// installed (read directly, so it's right even before the broker consent
// that Status().VhciDriver depends on), on macOS (no OS-level permission or
// driver of its own -- AttachAccessGranted is trivially always true there)
// whether the hardware USB/IP dongle is actually plugged in and answering.
func usbPermGranted(goos string, usb usbpass.Status, usbipInstalled func() bool) bool {
	if goos == "windows" {
		return usbipInstalled()
	}
	if goos == "darwin" {
		return usb.Dongle != nil && usb.Dongle.Error == ""
	}
	return usb.AttachGranted
}

// usbGrantedLabel is the granted-state button text: the default "Granted"
// everywhere except macOS, where it reads "HW USB" to make clear this is a
// physical dongle doing the work, not a virtual driver (there is nothing to
// grant on macOS -- see usbPermGranted).
func usbGrantedLabel(goos string) string {
	if goos == "darwin" {
		return loc().USBHardwareDongle
	}
	return ""
}

// showUSBDriverRow decides the separate "USB Passthrough Driver" install
// row. Never on Windows, where the USB Passthrough chip's own "Download"
// button covers it.
func showUSBDriverRow(goos string, usb usbpass.Status) bool {
	if goos == "windows" {
		return false
	}
	return usb.ConsentGiven && usb.Available && !usb.VhciDriver
}

// permRequestLabel is the not-granted button text for the driver-backed
// chips: "Download" on Windows, "Info" on macOS (there is nothing to grant
// there -- tapping it just explains that a hardware dongle is needed, see
// usbPermGranted/the usbAccessCheck onRequest handler), the default "Grant"
// ("") elsewhere.
func permRequestLabel(goos, download string) string {
	switch goos {
	case "windows":
		return download
	case "darwin":
		return loc().Info
	}
	return ""
}

// refreshPermRequestLabels (re)applies permRequestLabel -- at build time and
// on every language change.
func (w *Window) refreshPermRequestLabels() {
	label := permRequestLabel(runtime.GOOS, loc().PermDownload)
	w.usbAccessCheck.SetRequestLabel(label)
	w.vdisplayAccessCheck.SetRequestLabel(label)
}

func (w *Window) openUSBIPDriverDownload() {
	if parsed, err := url.Parse(usbipWin2ReleasesURL); err == nil {
		_ = w.app.OpenURL(parsed)
	}
}

// showUSBDongleInfoDialog is the macOS USB Passthrough chip's "Info" tap
// when no hardware dongle is plugged in yet. Replaces a bare
// showErrorDialog(driverHint) -- a flat "plug in the dongle" error string --
// with the same branded info-dialog chrome showWebClientInfoDialog uses:
// a short explanation of why macOS needs a physical dongle here at all,
// plus a CTA to the site's creative/pro section instead of leaving the
// user to wonder what the dongle is even for.
func (w *Window) showUSBDongleInfoDialog(parent fyne.Window) {
	if parent == nil {
		return
	}

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}

	body := widget.NewLabel(loc().USBDongleInfoBody)
	body.Wrapping = fyne.TextWrapWord
	body.Alignment = fyne.TextAlignLeading

	seeBtn := newDialogCTA(loc().USBDongleInfoCTA, func() {
		if parsed, err := url.Parse(usbridgeCreativeProURL); err == nil && w.app != nil {
			_ = w.app.OpenURL(parsed)
		}
	})

	footer := container.NewCenter(seeBtn)
	panel := newBrandedDialogPanelInsets(loc().USBDongleInfoTitle, statusDialogWidth, 20, 10, wrapDialogLabel(body, 11, design.ColorMutedOlive), footer, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}
