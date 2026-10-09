package usbpass

// RequiresProLicense mirrors rust-shine's crates/usb-passthrough/src/
// license_class.rs `classify` function -- kept in sync by hand, since this
// repo has no build-time link to that closed crate. Pro gates one device
// category, pen tablets: a device with an HID interface whose top-level
// usage page is a tablet/digitizer one. Everything else (keyboards, mice,
// gamepads, storage, audio, a device whose classes are unknown) is free.
//
// DISPLAY ONLY. This is never an enforcement decision: the real gate lives
// entirely in rust-shine's bin/usb-broker, which classifies from its own
// live OP_REQ_DEVLIST probe against the real exporter, not from anything
// this open-source client reports about itself. This only drives the
// dashboard's "Pro" badge (see controller/disk_widget_dashboard.go), so a
// wrong answer here misleads the user about what to expect, never what they
// can actually get.
//
// hidUsagePage is the device's top-level HID usage page (0 if unknown) --
// see models.USBPassthroughDevice.HIDUsagePage's doc comment: an HID
// interface class alone can't tell an ordinary mouse from a Wacom tablet.
// hidUsage is unused (as in license_class.rs's classify).
func RequiresProLicense(interfaces [][3]uint8, hidUsagePage, _ uint16) bool {
	if !isProHIDUsagePage(hidUsagePage) {
		return false
	}
	for _, iface := range interfaces {
		if iface[0] == hidClass {
			return true
		}
	}
	return false
}

func isProHIDUsagePage(page uint16) bool {
	for _, p := range proHIDUsagePages {
		if page == p {
			return true
		}
	}
	return false
}

// hidClass is the USB interface class of an HID interface (any
// subclass/protocol: a tablet's pen interface is a generic 03/00/00, but it
// may sit next to a boot-mouse 03/01/02 one).
const hidClass = 0x03

// proHIDUsagePages: see license_class.rs's PRO_HID_USAGE_PAGES -- the
// official USB HID Digitizers page, and Wacom's own vendor-specific page for
// its tablets' non-digitizer control interfaces.
var proHIDUsagePages = []uint16{
	0x0D,   // Digitizers (pens, touch digitizers)
	0xFF0D, // Wacom vendor-specific
}
