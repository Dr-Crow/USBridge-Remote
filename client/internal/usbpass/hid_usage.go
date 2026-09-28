package usbpass

import (
	"context"
	"sort"
	"time"

	"github.com/sirupsen/logrus"
)

// hidUsageProbeTimeout bounds each GET_DESCRIPTOR(Report) probeHIDUsage
// sends through a freshly claimed backend -- a device that never answers
// must not hold up StartSession.
const hidUsageProbeTimeout = 2 * time.Second

// parseHIDTopLevelUsages walks a HID report descriptor's short items and
// returns the (usage page, usage) of every top-level Application
// collection, in order. Long items are skipped; a malformed tail just ends
// the walk with whatever was collected so far.
func parseHIDTopLevelUsages(desc []byte) [][2]uint16 {
	var out [][2]uint16
	var page uint16
	var usages []uint32
	depth := 0
	for i := 0; i < len(desc); {
		prefix := desc[i]
		if prefix == 0xFE { // long item: 0xFE, bDataSize, bLongItemTag, data
			if i+1 >= len(desc) {
				break
			}
			i += 3 + int(desc[i+1])
			continue
		}
		size := int(prefix & 0x03)
		if size == 3 {
			size = 4
		}
		if i+1+size > len(desc) {
			break
		}
		var val uint32
		for k := 0; k < size; k++ {
			val |= uint32(desc[i+1+k]) << (8 * k)
		}
		i += 1 + size
		switch prefix & 0xFC {
		case 0x04: // Usage Page (global)
			page = uint16(val)
		case 0x08: // Usage (local); a 4-byte usage carries its own page
			if size == 4 {
				usages = append(usages, val)
			} else {
				usages = append(usages, uint32(page)<<16|val)
			}
		case 0xA0: // Collection
			if depth == 0 && val == 0x01 && len(usages) > 0 {
				u := usages[0]
				out = append(out, [2]uint16{uint16(u >> 16), uint16(u)})
			}
			depth++
			usages = usages[:0]
		case 0xC0: // End Collection
			if depth > 0 {
				depth--
			}
			usages = usages[:0]
		case 0x80, 0x90, 0xB0: // Input/Output/Feature end the local scope
			usages = usages[:0]
		}
	}
	return out
}

// pickHIDUsage reduces a device's top-level usages to the single pair
// ExportedDevice.HIDUsagePage/HIDUsage carries: a GamePad/Joystick usage
// wins (that is what the pair exists to report), otherwise the first one.
func pickHIDUsage(usages [][2]uint16) (page, usage uint16) {
	for _, u := range usages {
		if u[0] == 0x01 && (u[1] == 0x04 || u[1] == 0x05) {
			return u[0], u[1]
		}
	}
	if len(usages) > 0 {
		return usages[0][0], usages[0][1]
	}
	return 0, 0
}

// hidInterfaceReportLens lists every HID interface in a raw configuration
// descriptor as interface number -> report descriptor length (from the HID
// class descriptor that follows the INTERFACE descriptor; 0 if absent).
func hidInterfaceReportLens(cfg []byte) map[uint8]int {
	out := map[uint8]int{}
	cur := -1
	for i := 0; i+2 <= len(cfg); {
		length := int(cfg[i])
		if length < 2 || i+length > len(cfg) {
			break
		}
		switch cfg[i+1] {
		case 0x04: // INTERFACE
			cur = -1
			if length >= 9 && cfg[i+5] == 0x03 {
				cur = int(cfg[i+2])
				if _, ok := out[uint8(cur)]; !ok {
					out[uint8(cur)] = 0
				}
			}
		case 0x21: // HID: bNumDescriptors at 5, then (bDescriptorType, wLength) pairs
			if cur >= 0 && length >= 9 {
				for off := 6; off+3 <= length; off += 3 {
					if cfg[i+off] == 0x22 {
						out[uint8(cur)] = int(cfg[i+off+1]) | int(cfg[i+off+2])<<8
						break
					}
				}
			}
		}
		i += length
	}
	return out
}

// probeHIDUsage fills ed.HIDUsagePage/HIDUsage, when the lister left them
// unknown, by asking ed's own (already claimed) backend for each HID
// interface's report descriptor. Only list_hid_darwin.go and listSysfs can
// supply the pair at list time; this covers every other backend (Windows
// hidbridge/WinUSB, libusb) so a generic 03/00/00 gamepad is not reported
// as usage 0/0 -- which rust-shine's license classify() treats as Pro.
func probeHIDUsage(ed *ExportedDevice) {
	if ed == nil || ed.Backend == nil || ed.HIDUsagePage != 0 {
		return
	}
	lens := hidInterfaceReportLens(ed.ConfigDesc)
	ifnums := make([]int, 0, len(lens))
	for n := range lens {
		ifnums = append(ifnums, int(n))
	}
	sort.Ints(ifnums)
	var usages [][2]uint16
	for _, n := range ifnums {
		ifnum, repLen := uint8(n), lens[uint8(n)]
		if repLen <= 0 || repLen > 4096 {
			repLen = 4096
		}
		setup := [8]byte{0x81, 0x06, 0x00, 0x22, ifnum, 0x00, byte(repLen), byte(repLen >> 8)}
		ctx, cancel := context.WithTimeout(context.Background(), hidUsageProbeTimeout)
		status, data := ed.Backend.HandleControl(ctx, setup, repLen, nil)
		cancel()
		if status != 0 || len(data) == 0 {
			logrus.Debugf("usbpass: HID usage probe %s iface %d: status=%d len=%d", ed.BusID, ifnum, status, len(data))
			continue
		}
		usages = append(usages, parseHIDTopLevelUsages(data)...)
	}
	ed.HIDUsagePage, ed.HIDUsage = pickHIDUsage(usages)
	if ed.HIDUsagePage != 0 {
		logrus.Infof("usbpass: %s HID top-level usage %04x/%04x", ed.BusID, ed.HIDUsagePage, ed.HIDUsage)
	}
}
