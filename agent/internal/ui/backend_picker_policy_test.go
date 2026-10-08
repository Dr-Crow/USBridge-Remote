package ui

import "testing"

func TestBackendPickerHasOneUSBridgeOption(t *testing.T) {
	n := 0
	for _, option := range protocolOptions {
		if option.key == protocolPro || option.key == protocolEnterprise {
			t.Fatal("paid tier offered as a separate backend")
		}
		if option.key == protocolFree {
			n++
			if option.badge != "" || option.label != "USBridge streamer" {
				t.Fatal("streamer still marketed as a tier")
			}
		}
	}
	if n != 1 {
		t.Fatalf("got %d USBridge options", n)
	}
	for _, tier := range []string{protocolFree, protocolPro, protocolEnterprise} {
		if protocolPickerKey(tier) != protocolFree {
			t.Fatalf("tier %s not mapped to the single backend", tier)
		}
	}
}
