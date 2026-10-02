package controller

import (
	"os"

	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
	"usbridge-client/internal/usbpass"
)

// rawHIDStreamLink is the video stream as usbpass.RawHIDLink: it carries a
// tablet's input over the stream's control channel instead of USB/IP (see
// usbpass/rawhid.go). The stream is looked up on every call, so the link
// outlives reconnects.
type rawHIDStreamLink struct{ dw *DiskWidget }

func (l rawHIDStreamLink) sender() service.MoonlightRawHIDSender {
	if l.dw.moonlightProvider == nil {
		return nil
	}
	s, _ := l.dw.moonlightProvider().(service.MoonlightRawHIDSender)
	return s
}

func (l rawHIDStreamLink) RawHIDEpoch() uint64 {
	if s := l.sender(); s != nil {
		return s.RawHIDEpoch()
	}
	return 0
}

func (l rawHIDStreamLink) SendRawHID(kind, slot, endpoint uint8, total, offset uint16, data []byte, reliable bool) bool {
	if s := l.sender(); s != nil {
		return s.SendMoonlightRawHID(kind, slot, endpoint, total, offset, data, reliable)
	}
	return false
}

// SetRawHIDSupported records whether the paired agent's active streaming
// backend understands LiSendRawHidEvent (MasterSyncResponse.RawHIDSupported,
// refreshed on every master sync -- see main_window_sync_v2.go).
// rust-shine's own streamer and a punktfunk-host with the USBridge patch
// implement the host side (see streamhost.CodecProbe.RawHIDSupported's doc
// comment in the agent repo); Sunshine and stock Punktfunk do not, so
// splitRawHID must not pick stream mode against them.
func (dw *DiskWidget) SetRawHIDSupported(v bool) {
	dw.rawHIDSupportedByHost = v
}

// splitRawHID separates the devices that go over the stream from those that
// are exported over USB/IP. A device goes over the stream only while a
// stream is up (RawHIDEpoch != 0) whose host's streamer actually understands
// LiSendRawHidEvent (rawHIDSupportedByHost) -- without the latter check, a
// Sunshine or stock Punktfunk stream looks exactly like a rust-shine one from the
// epoch alone, the client would switch a Wacom tablet into stream mode
// against a host that cannot rebuild it, and the tablet would silently stop
// working: once stream mode is chosen there is no USB/IP fallback.
// USBRIDGE_RAW_HID=0 keeps everything on USB/IP regardless.
func (dw *DiskWidget) splitRawHID(devices []models.USBPassthroughDevice) (raw, export []models.USBPassthroughDevice) {
	if os.Getenv("USBRIDGE_RAW_HID") == "0" || !dw.rawHIDSupportedByHost || (rawHIDStreamLink{dw}).RawHIDEpoch() == 0 {
		return nil, devices
	}
	for _, d := range devices {
		if usbpass.RawHIDEligible(d) {
			raw = append(raw, d)
		} else {
			export = append(export, d)
		}
	}
	return raw, export
}
