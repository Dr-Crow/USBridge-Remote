package usbpass

import "testing"

var (
	bootKeyboard  = [3]uint8{0x03, 0x01, 0x01}
	bootMouse     = [3]uint8{0x03, 0x01, 0x02}
	xinputGamepad = [3]uint8{0xFF, 0x5D, 0x01}
	gipGamepad    = [3]uint8{0xFF, 0x47, 0xD0}
	massStorage   = [3]uint8{0x08, 0x06, 0x50}
	audioClass    = [3]uint8{0x01, 0x01, 0x00}
	genericHID    = [3]uint8{0x03, 0x00, 0x00}
)

// x360SyntheticComposite is byte-for-byte what x360_backend.go's
// ExportedDevice declares for every XInput-shaped gamepad export -- same
// shape license_class.rs's identical test fixture in rust-shine reproduces.
var x360SyntheticComposite = [][3]uint8{
	{0xFF, 0x5D, 0x01},
	{0xFF, 0x5D, 0x03},
	{0xFF, 0x5D, 0x02},
	{0xFF, 0xFD, 0x13},
}

func TestRequiresProLicense_InputDevicesAreFree(t *testing.T) {
	cases := []struct {
		name  string
		ifs   [][3]uint8
		page  uint16
		usage uint16
	}{
		{"x360 composite", x360SyntheticComposite, 0, 0},
		{"boot keyboard", [][3]uint8{bootKeyboard}, 0, 0},
		{"boot mouse", [][3]uint8{bootMouse}, 0, 0},
		{"keyboard+mouse dongle", [][3]uint8{bootKeyboard, bootMouse}, 0x01, 0x06},
		{"xinput", [][3]uint8{xinputGamepad}, 0, 0},
		{"gip", [][3]uint8{gipGamepad}, 0, 0},
		{"generic HID mouse", [][3]uint8{genericHID}, 0x01, 0x02},
		{"generic HID gamepad", [][3]uint8{genericHID}, 0x01, 0x05},
	}
	for _, c := range cases {
		if RequiresProLicense(c.ifs, c.page, c.usage) {
			t.Errorf("%s should be free", c.name)
		}
	}
}

func TestRequiresProLicense_OtherDevicesAreFree(t *testing.T) {
	cases := []struct {
		name string
		ifs  [][3]uint8
		page uint16
	}{
		{"mass storage", [][3]uint8{massStorage}, 0},
		{"audio", [][3]uint8{audioClass}, 0},
		{"keyboard+storage composite", [][3]uint8{bootKeyboard, massStorage}, 0x01},
		{"unknown classes", nil, 0},
		{"generic HID, no usage page", [][3]uint8{genericHID}, 0},
		{"digitizer page without HID", [][3]uint8{massStorage}, 0x0D},
	}
	for _, c := range cases {
		if RequiresProLicense(c.ifs, c.page, 0) {
			t.Errorf("%s should be free", c.name)
		}
	}
}

func TestRequiresProLicense_TabletsArePro(t *testing.T) {
	if !RequiresProLicense([][3]uint8{genericHID}, 0x0D, 0x02) {
		t.Error("an HID digitizer should require Pro")
	}
	if !RequiresProLicense([][3]uint8{genericHID}, 0xFF0D, 0x01) {
		t.Error("a Wacom vendor-page HID device should require Pro")
	}
	if !RequiresProLicense([][3]uint8{bootMouse, genericHID}, 0x0D, 0x02) {
		t.Error("a tablet that also presents a boot mouse should require Pro")
	}
}
