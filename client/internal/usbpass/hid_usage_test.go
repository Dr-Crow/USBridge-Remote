package usbpass

import (
	"context"
	"reflect"
	"testing"
)

// Opening of a DualShock 4 style report descriptor (Razer Raiju 1532:1007
// uses the same layout): Generic Desktop / GamePad application collection.
var ds4ReportDescHead = []byte{
	0x05, 0x01, // Usage Page (Generic Desktop)
	0x09, 0x05, // Usage (Game Pad)
	0xA1, 0x01, // Collection (Application)
	0x85, 0x01, //   Report ID 1
	0x09, 0x30, //   Usage (X)
	0x09, 0x31, //   Usage (Y)
	0x15, 0x00, //   Logical Minimum 0
	0x26, 0xFF, 0x00, // Logical Maximum 255
	0x75, 0x08, //   Report Size 8
	0x95, 0x02, //   Report Count 2
	0x81, 0x02, //   Input (Data,Var,Abs)
	0x06, 0x00, 0xFF, // Usage Page (Vendor 0xFF00)
	0x09, 0x21, //   Usage (0x21)
	0xA1, 0x02, //   Collection (Logical) -- nested, not top-level
	0xC0, //   End Collection
	0xC0, // End Collection
}

func TestParseHIDTopLevelUsages(t *testing.T) {
	got := parseHIDTopLevelUsages(ds4ReportDescHead)
	want := [][2]uint16{{0x01, 0x05}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
}

func TestParseHIDTopLevelUsagesMultipleAndExtendedUsage(t *testing.T) {
	desc := []byte{
		0x05, 0x01, 0x09, 0x02, 0xA1, 0x01, 0xC0, // Mouse
		0x0B, 0x01, 0x00, 0x0C, 0x00, 0xA1, 0x01, 0xC0, // 4-byte usage 000C:0001
		0x05, 0x01, 0x09, 0x04, 0xA1, 0x01, 0xC0, // Joystick
	}
	got := parseHIDTopLevelUsages(desc)
	want := [][2]uint16{{0x01, 0x02}, {0x0C, 0x01}, {0x01, 0x04}}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("got %v want %v", got, want)
	}
	if p, u := pickHIDUsage(got); p != 0x01 || u != 0x04 {
		t.Fatalf("pick = %04x/%04x, want gamepad/joystick preferred", p, u)
	}
}

func TestParseHIDTopLevelUsagesTruncated(t *testing.T) {
	if got := parseHIDTopLevelUsages([]byte{0x05, 0x01, 0x09}); len(got) != 0 {
		t.Fatalf("got %v", got)
	}
}

type reportDescBackend struct {
	desc  []byte
	setup [8]byte
}

func (b *reportDescBackend) HandleControl(_ context.Context, setup [8]byte, wLength int, _ []byte) (int32, []byte) {
	b.setup = setup
	return 0, b.desc
}
func (b *reportDescBackend) HandleBulk(context.Context, uint8, bool, int, []byte) (int32, []byte) {
	return -32, nil
}
func (b *reportDescBackend) Close() error { return nil }

func TestProbeHIDUsageFromBackend(t *testing.T) {
	cfg := []byte{
		0x09, 0x02, 0x29, 0x00, 0x01, 0x01, 0x00, 0x80, 0xFA, // CONFIGURATION
		0x09, 0x04, 0x00, 0x00, 0x02, 0x03, 0x00, 0x00, 0x00, // INTERFACE 0, HID 03/00/00
		0x09, 0x21, 0x11, 0x01, 0x00, 0x01, 0x22, byte(len(ds4ReportDescHead)), 0x00, // HID
		0x07, 0x05, 0x84, 0x03, 0x40, 0x00, 0x05, // EP IN
	}
	be := &reportDescBackend{desc: ds4ReportDescHead}
	ed := &ExportedDevice{BusID: "8-149", ConfigDesc: cfg, Backend: be}
	probeHIDUsage(ed)
	if ed.HIDUsagePage != 0x01 || ed.HIDUsage != 0x05 {
		t.Fatalf("usage = %04x/%04x, want 0001/0005", ed.HIDUsagePage, ed.HIDUsage)
	}
	want := [8]byte{0x81, 0x06, 0x00, 0x22, 0x00, 0x00, byte(len(ds4ReportDescHead)), 0x00}
	if be.setup != want {
		t.Fatalf("setup = % x, want % x", be.setup, want)
	}
}
