package streamhost

import (
	"context"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"usbridge_agent/internal/usbpass"
)

// punktfunkBrokerEnv tells a punktfunk-host built with the USBridge patch
// (crates/punktfunk-host/src/gamestream/usbridge.rs) where the USB broker's
// control port is. With it the host forwards a client's raw HID device (a
// Wacom tablet sent over the stream, LiSendRawHidEvent) to the broker's
// hid_stream, which rebuilds it on a USB/IP port -- the same device
// rust-shine's own streamer builds in-process -- and, on Windows, its
// gamepads too (the agent installs usbip-win2, not Punktfunk's pad drivers).
// A stock punktfunk-host ignores the variable. rust-shine's streamer reads
// the same name.
const punktfunkBrokerEnv = "USBRIDGE_USB_BROKER_CONTROL"

// punktfunkBridgeProbe is the subcommand a patched punktfunk-host answers
// with punktfunkBridgeLine and exit 0. A stock one exits 1 with "unknown
// command" (confirmed against 0.42.0).
const (
	punktfunkBridgeProbe = "usbridge-bridge"
	punktfunkBridgeLine  = "usbridge-bridge 1"
)

var punktfunkBridgeCache struct {
	sync.Mutex
	bin     string
	modTime time.Time
	has     bool
}

// punktfunkHasBridge reports whether the binary at bin carries the USBridge
// patch. Asked once per binary (path and mtime): the answer only changes
// when the file does.
func punktfunkHasBridge(bin string) bool {
	if bin == "" {
		return false
	}
	st, err := os.Stat(bin)
	if err != nil {
		return false
	}
	c := &punktfunkBridgeCache
	c.Lock()
	defer c.Unlock()
	if c.bin == bin && c.modTime.Equal(st.ModTime()) {
		return c.has
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, bin, punktfunkBridgeProbe).Output()
	c.bin, c.modTime = bin, st.ModTime()
	c.has = err == nil && strings.Contains(string(out), punktfunkBridgeLine)
	return c.has
}

// punktfunkBrokerEnviron is what Start adds to punktfunk-host's environment.
func punktfunkBrokerEnviron() string {
	return punktfunkBrokerEnv + "=" + usbpass.DefaultControlAddr
}
