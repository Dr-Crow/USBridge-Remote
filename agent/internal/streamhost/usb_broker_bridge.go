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

// usbBrokerEnv tells a streamer built with the USBridge patch where the USB
// broker's control port is: punktfunk-host
// (crates/punktfunk-host/src/gamestream/usbridge.rs) and Sunshine
// (src/usbridge.cpp), both in the Streamers-Forks repository. With it the
// host forwards a client's raw HID device (a Wacom tablet sent over the
// stream, LiSendRawHidEvent) to the broker's hid_stream, which rebuilds it on
// a USB/IP port -- the same device rust-shine's own streamer builds
// in-process -- and, on Windows, its gamepads too (the agent installs
// usbip-win2, not ViGEmBus or Punktfunk's pad drivers). A stock build ignores
// the variable. rust-shine's streamer reads the same name.
const usbBrokerEnv = "USBRIDGE_USB_BROKER_CONTROL"

// usbBridgeLine is what a patched streamer prints, with exit 0, when asked
// with its probe argument (`punktfunk-host usbridge-bridge`,
// `sunshine --usbridge-bridge`). A stock one fails with "unknown command"
// (confirmed against punktfunk-host 0.42.0).
const usbBridgeLine = "usbridge-bridge 1"

var usbBridgeCache struct {
	sync.Mutex
	seen map[string]usbBridgeAnswer
}

type usbBridgeAnswer struct {
	modTime time.Time
	has     bool
}

// streamerHasUSBBridge reports whether the streamer binary at bin carries
// the USBridge patch. Asked once per binary (path and mtime): the answer
// only changes when the file does. prepare, if not nil, adjusts the probe
// command before it runs (hiding a console window on Windows).
func streamerHasUSBBridge(bin, probeArg string, prepare func(*exec.Cmd)) bool {
	if bin == "" {
		return false
	}
	st, err := os.Stat(bin)
	if err != nil {
		return false
	}
	c := &usbBridgeCache
	c.Lock()
	defer c.Unlock()
	if a, ok := c.seen[bin]; ok && a.modTime.Equal(st.ModTime()) {
		return a.has
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, bin, probeArg)
	if prepare != nil {
		prepare(cmd)
	}
	out, err := cmd.Output()
	has := err == nil && strings.Contains(string(out), usbBridgeLine)
	if c.seen == nil {
		c.seen = map[string]usbBridgeAnswer{}
	}
	c.seen[bin] = usbBridgeAnswer{modTime: st.ModTime(), has: has}
	return has
}

// usbBrokerEnviron is what a backend's Start adds to its streamer's
// environment.
func usbBrokerEnviron() string {
	return usbBrokerEnv + "=" + usbpass.DefaultControlAddr
}
