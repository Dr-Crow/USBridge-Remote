package usbpass

import "testing"

var (
	bootKeyboard  = [3]uint8{0x03, 0x01, 0x01}
	bootMouse     = [3]uint8{0x03, 0x01, 0x02}
	xinputGamepad = [3]uint8{0xFF, 0x5D, 0x01}
	gipGamepad    = [3]uint8{0xFF, 0x47, 0xD0}
	massStorage   = [3]uint8{0x08, 0x06, 0x50}
	genericHID    = [3]uint8{0x03, 0x00, 0x00}
)

// x360SyntheticComposite is byte-for-byte what x360_backend.go's
// ExportedDevice declares for every XInput-shaped gamepad export -- same
// shape license_class.rs's identical test fixture in rust-shine reproduces,
// kept in sync by hand same as RequiresProLicense itself.
var x360SyntheticComposite = [][3]uint8{
	{0xFF, 0x5D, 0x01},
	{0xFF, 0x5D, 0x03},
	{0xFF, 0x5D, 0x02},
	{0xFF, 0xFD, 0x13},
}

func TestRequiresProLicense_X360SyntheticCompositeIsFree(t *testing.T) {
	if RequiresProLicense(x360SyntheticComposite, 0, 0) {
		t.Error("expected the full XInput synthetic composite to be free")
	}
}

func TestRequiresProLicense_BootKeyboardAndMouseAreFree(t *testing.T) {
	if RequiresProLicense([][3]uint8{bootKeyboard}, 0, 0) {
		t.Error("boot keyboard alone should be free")
	}
	if RequiresProLicense([][3]uint8{bootMouse}, 0, 0) {
		t.Error("boot mouse alone should be free")
	}
	if RequiresProLicense([][3]uint8{bootKeyboard, bootMouse}, 0, 0) {
		t.Error("a composite boot keyboard+mouse dongle should be free")
	}
}

func TestRequiresProLicense_XInputAndGIPGamepadsAreFree(t *testing.T) {
	if RequiresProLicense([][3]uint8{xinputGamepad}, 0, 0) {
		t.Error("XInput gamepad interface alone should be free")
	}
	if RequiresProLicense([][3]uint8{gipGamepad}, 0, 0) {
		t.Error("GIP gamepad interface alone should be free")
	}
}

func TestRequiresProLicense_MassStorageAndAudioArePro(t *testing.T) {
	if !RequiresProLicense([][3]uint8{massStorage}, 0, 0) {
		t.Error("mass storage should require Pro")
	}
}

func TestRequiresProLicense_GenericHIDWithoutUsagePageIsPro(t *testing.T) {
	// Probe/report couldn't determine a usage page -- ambiguous resolves to
	// Pro, same as a tablet/digitizer page, never a guess.
	if !RequiresProLicense([][3]uint8{genericHID}, 0, 0) {
		t.Error("generic HID with no usage page info should require Pro")
	}
}

// TestRequiresProLicense_RazerViperMouseIsFree is the exact reported bug:
// a Razer Viper (plain USB mouse, no boot-mouse interface at all -- 03/00/00
// + Generic Desktop Mouse usage) showed "Pro" on the dashboard next to
// actual Wacom tablets under the old allowlist design, which only ever
// covered Generic Desktop Joystick/GamePad.
func TestRequiresProLicense_RazerViperMouseIsFree(t *testing.T) {
	if RequiresProLicense([][3]uint8{genericHID}, 0x01, 0x02) {
		t.Error("a generic-HID mouse (Generic Desktop page, Mouse usage) should be free")
	}
}

func TestRequiresProLicense_GenericHIDKeyboardIsFree(t *testing.T) {
	if RequiresProLicense([][3]uint8{genericHID}, 0x01, 0x06) {
		t.Error("a generic-HID keyboard should be free")
	}
}

func TestRequiresProLicense_GenericHIDGamepadAndJoystickAreFree(t *testing.T) {
	if RequiresProLicense([][3]uint8{genericHID}, 0x01, 0x05) {
		t.Error("a generic-HID gamepad should be free")
	}
	if RequiresProLicense([][3]uint8{genericHID}, 0x01, 0x04) {
		t.Error("a generic-HID joystick should be free")
	}
}

// TestRequiresProLicense_AnyOtherGenericDesktopUsageIsFree is the whole
// point of the blocklist redesign: a usage this code has never specifically
// heard of still shouldn't require Pro, because Generic Desktop isn't a
// tablet/digitizer page -- unlike an allowlist, a device shape doesn't need
// to be individually recognized to stay free.
func TestRequiresProLicense_AnyOtherGenericDesktopUsageIsFree(t *testing.T) {
	if RequiresProLicense([][3]uint8{genericHID}, 0x01, 0x80) {
		t.Error("an unrecognized Generic Desktop usage should still be free")
	}
}

func TestRequiresProLicense_DigitizerUsagePageIsPro(t *testing.T) {
	if !RequiresProLicense([][3]uint8{genericHID}, 0x0D, 0x02) {
		t.Error("the official HID Digitizers usage page should require Pro")
	}
}

func TestRequiresProLicense_WacomVendorUsagePageIsPro(t *testing.T) {
	if !RequiresProLicense([][3]uint8{genericHID}, 0xFF0D, 0x01) {
		t.Error("Wacom's vendor-specific usage page should require Pro")
	}
}

func TestRequiresProLicense_CompositeWithOneNonFreeInterfaceIsPro(t *testing.T) {
	if !RequiresProLicense([][3]uint8{bootKeyboard, massStorage}, 0, 0) {
		t.Error("a single non-free interface should disqualify the whole device")
	}
	interfaces := append([][3]uint8{}, x360SyntheticComposite...)
	interfaces = append(interfaces, massStorage)
	if !RequiresProLicense(interfaces, 0, 0) {
		t.Error("a non-free interface added to the XInput composite should still require Pro")
	}
}

func TestRequiresProLicense_EmptyInterfaceListIsPro(t *testing.T) {
	if !RequiresProLicense(nil, 0x01, 0x05) {
		t.Error("an empty interface list should require Pro regardless of usage")
	}
}
