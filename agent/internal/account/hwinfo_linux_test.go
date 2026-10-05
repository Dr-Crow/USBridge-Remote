//go:build linux

package account

import "testing"

func TestShortGPUName_ExtractsMarketingNameFromBrackets(t *testing.T) {
	cases := []struct {
		vendor, device, want string
	}{
		// The exact real-world pci.ids shape reported live: "GPU NVIDIA
		// Corporation TU102 [GeForce RTX 2080]" on the client's Net Graph
		// HUD was long enough to push the decoder/codec text off the line.
		{"NVIDIA Corporation", "TU102 [GeForce RTX 2080]", "GeForce RTX 2080"},
		{"NVIDIA Corporation", "GA104 [GeForce RTX 3070]", "GeForce RTX 3070"},
		{"Intel Corporation", "UHD Graphics 630 [8086:3e92]", "8086:3e92"},
		// No bracket at all -- falls back to the full vendor+device string,
		// same as before this existed.
		{"Advanced Micro Devices, Inc. [AMD/ATI]", "Raphael", "Advanced Micro Devices, Inc. [AMD/ATI] Raphael"},
		// A bracket with nothing but whitespace inside must not produce an
		// empty GPU name.
		{"Vendor", "Device [ ]", "Vendor Device [ ]"},
	}
	for _, c := range cases {
		if got := shortGPUName(c.vendor, c.device); got != c.want {
			t.Errorf("shortGPUName(%q, %q) = %q, want %q", c.vendor, c.device, got, c.want)
		}
	}
}
