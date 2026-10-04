package usbpass

// RequiresProLicense mirrors rust-shine's crates/usb-passthrough/src/
// license_class.rs `classify` function byte for byte (same
// freeInterfaceClasses triples, same tablet/digitizer usage-page blocklist,
// same "all interfaces must be free, unknown defaults to Pro" rule) -- kept
// in sync by hand, since this repo has no build-time link to that closed
// crate.
//
// DISPLAY ONLY. This is never an enforcement decision: the real gate lives
// entirely in rust-shine's bin/usb-broker, which classifies from its own
// live OP_REQ_DEVLIST probe against the real exporter, not from anything
// this open-source client reports about itself. A modified client could
// make this function return whatever it wants -- the actual attach would
// still be accepted or refused by rust-shine exactly the same either way.
// This only drives the dashboard's "Pro" badge (see
// controller/disk_widget_dashboard.go), so a wrong answer here misleads the
// user about what to expect, never what they can actually get.
//
// hidUsagePage is the device's top-level HID usage page (0 if unknown) --
// see models.USBPassthroughDevice.HIDUsagePage's doc comment for why this
// is needed at all: a generic HID interface (03/00/00) alone can't tell an
// ordinary mouse/keyboard/gamepad from a Wacom-class tablet. hidUsage is no
// longer used by this decision (same as license_class.rs's classify, which
// keeps the parameter only so its signature doesn't need to change) --
// kept so every call site here doesn't need touching if a future usage
// ever does matter again.
func RequiresProLicense(interfaces [][3]uint8, hidUsagePage, _ uint16) bool {
	if len(interfaces) == 0 {
		return true // unknown -> Pro, same default as license_class.rs
	}
	genericHIDIsFree := hidUsagePage != 0 && !isProGenericHIDUsagePage(hidUsagePage)
	for _, iface := range interfaces {
		if isFreeInterfaceClass(iface) {
			continue
		}
		if iface == genericHIDClass && genericHIDIsFree {
			continue
		}
		return true
	}
	return false
}

func isFreeInterfaceClass(iface [3]uint8) bool {
	for _, free := range freeInterfaceClasses {
		if iface == free {
			return true
		}
	}
	return false
}

func isProGenericHIDUsagePage(page uint16) bool {
	for _, p := range proGenericHIDUsagePages {
		if page == p {
			return true
		}
	}
	return false
}

// freeInterfaceClasses: see license_class.rs's FREE_INTERFACE_CLASSES doc
// comment for why the three FF/5D/02, FF/5D/03, FF/FD/13 entries beyond the
// primary FF/5D/01 control interface are included -- companion interfaces
// (plugin module, headset/voice, Xbox Security Method) every XInput-shaped
// gamepad export presents alongside interface 0 in a 4-interface composite,
// not separate device kinds. Omitting them here would just mean this
// dashboard badge disagrees with the real enforcement for every such
// device, not that the device is actually blocked.
var freeInterfaceClasses = [][3]uint8{
	{0x03, 0x01, 0x01}, // HID boot keyboard
	{0x03, 0x01, 0x02}, // HID boot mouse
	{0xFF, 0x5D, 0x01}, // XInput (Xbox 360) gamepad: control interface
	{0xFF, 0x5D, 0x02}, // XInput (Xbox 360) gamepad: plugin module interface
	{0xFF, 0x5D, 0x03}, // XInput (Xbox 360) gamepad: headset/voice interface
	{0xFF, 0xFD, 0x13}, // XInput (Xbox 360) gamepad: security method interface
	{0xFF, 0x47, 0xD0}, // GIP (Xbox One/Series) gamepad
}

// genericHIDClass covers everything from an ordinary non-boot-protocol
// mouse/keyboard to a DirectInput gamepad to a Wacom tablet -- free unless
// hidUsagePage is one of proGenericHIDUsagePages, checked by
// RequiresProLicense's caller. See license_class.rs's GENERIC_HID_INTERFACE
// doc comment for why this is a blocklist of the one gated device category
// (tablet/digitizer), not an allowlist of every HID usage an ordinary free
// device might report -- the previous allowlist design (only Generic
// Desktop Joystick/GamePad) is exactly what let a Razer Viper (plain USB
// mouse, no boot-mouse interface) show "Pro" on this dashboard next to
// actual Wacom tablets, even though stated policy has only ever been
// "Pro gates Wacom-class devices and 4:4:4 color, nothing else."
var genericHIDClass = [3]uint8{0x03, 0x00, 0x00}

// proGenericHIDUsagePages: see license_class.rs's PRO_GENERIC_HID_USAGE_PAGES
// doc comment -- the official USB HID Digitizers page, and Wacom's own
// vendor-specific page for its tablets' non-digitizer control interfaces.
var proGenericHIDUsagePages = []uint16{
	0x0D,   // Digitizers (pens, touch digitizers)
	0xFF0D, // Wacom vendor-specific
}
