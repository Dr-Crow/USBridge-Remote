//go:build linux

package app

import (
	"os"
	"strings"

	"usbridge_agent/internal/display"
)

const nvidiaPCIVendor = "0x10de"

// sunshineEncoderPin returns the "encoder" value Sunshine must be pinned to
// on this host, or "" to leave its auto-probe alone.
//
// "nvenc" when every connected physical monitor sits on an NVIDIA card.
// Sunshine's KMS capture numbers monitors by counting active planes, and
// its CUDA (NVENC) path skips non-NVIDIA cards unless encoder = nvenc is
// set explicitly -- so a leftover vkms card (a removed or forced-off
// virtual display) shifts the NVENC index against the "Monitor N is ..."
// list output_name is derived from. NVENC then fails "Couldn't find
// monitor [N]" and the probe falls back to the Vulkan encoder, which
// segfaults as soon as a client starts a stream (confirmed live: a
// restart loop every ~3s for hours, with the KMS grant itself fine). The
// pin makes the NVENC path count every card like the monitor list does,
// and keeps the probe from falling back to Vulkan at all.
func sunshineEncoderPin() string {
	var names []string
	for _, c := range display.Connectors() {
		names = append(names, c.Name)
	}
	return encoderPinFor(names, func(card string) string {
		b, _ := os.ReadFile("/sys/class/drm/" + card + "/device/vendor")
		return strings.TrimSpace(string(b))
	})
}

// encoderPinFor is sunshineEncoderPin over connected DRM connector names
// ("card1-HDMI-A-1") and a card -> PCI vendor lookup.
func encoderPinFor(connectors []string, vendorOf func(card string) string) string {
	physical := 0
	for _, name := range connectors {
		m := drmConnectorRe.FindStringSubmatch(name)
		if m == nil || strings.HasPrefix(m[2], "Virtual-") {
			continue
		}
		if vendorOf(m[1]) != nvidiaPCIVendor {
			return ""
		}
		physical++
	}
	if physical == 0 {
		return ""
	}
	return "nvenc"
}
