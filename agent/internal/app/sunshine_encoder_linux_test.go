//go:build linux

package app

import "testing"

func TestEncoderPinFor(t *testing.T) {
	vendors := map[string]string{"card0": "", "card1": nvidiaPCIVendor, "card2": "0x8086"}
	vendorOf := func(card string) string { return vendors[card] }
	cases := []struct {
		name       string
		connectors []string
		want       string
	}{
		{"nvidia only", []string{"card1-HDMI-A-1"}, "nvenc"},
		{"leftover vkms ignored", []string{"card0-Virtual-1", "card1-HDMI-A-1"}, "nvenc"},
		{"hybrid keeps auto", []string{"card1-HDMI-A-1", "card2-eDP-1"}, ""},
		{"intel only", []string{"card2-eDP-1"}, ""},
		{"virtual only", []string{"card0-Virtual-1"}, ""},
		{"nothing connected", nil, ""},
	}
	for _, c := range cases {
		if got := encoderPinFor(c.connectors, vendorOf); got != c.want {
			t.Errorf("%s: encoderPinFor(%v) = %q, want %q", c.name, c.connectors, got, c.want)
		}
	}
}
