//go:build js && wasm

package api

import (
	"fmt"
	"net"
	"testing"
)

// TestUSBClientSetOpenDataChannelNeverFallsBackOnWasm pins the wasm-only
// half of the fix: the web client has no legitimate direct-fetch fallback
// (that's desktop-native only, see usb_client_fallback_default.go) -- a
// failed/unopened DataChannel must error out immediately instead of
// attempting a network request that a relay/remote browser session always
// has blocked (mixed content or Local Network Access), which is what used
// to show up as an endless stream of "(blocked)" devtools entries.
func TestUSBClientSetOpenDataChannelNeverFallsBackOnWasm(t *testing.T) {
	if usbClientDataChannelFallback {
		t.Fatal("usbClientDataChannelFallback must be false on the wasm build")
	}

	client := NewUSBClient("192.0.2.1", 1, 1) // TEST-NET-1, RFC 5737 -- never dialed
	client.SetOpenDataChannel(func(label string) (net.Conn, error) {
		return nil, fmt.Errorf("no peer connection yet")
	})

	if _, err := client.makeRequest("GET", "/api/status", nil); err == nil {
		t.Fatal("expected the request to fail outright instead of falling back to a network fetch")
	}
}
