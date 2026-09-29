// Package usbpass: in-process USB/IP v1.1.1 export server (no external
// usbipd-win) plus a pure-Go AES-GCM client for the closed rust-shine
// agent's control plane (see usbaes_attach.go). Device listing is native
// per platform (see ListLocal); the client no longer uses the closed
// usbridge-usb-broker binary at all.
package usbpass

import (
	"context"
	"fmt"
	"hash/fnv"
	"net"
	"runtime"
	"strconv"

	"usbridge-client/internal/models"
)

// parseHexTriple parses three 2-digit hex strings (a device's
// bInterfaceClass/SubClass/Protocol, however the platform's enumeration API
// happened to surface them -- sysfs text files on Linux, "Class_xx&
// SubClass_yy&Prot_zz" compatible-ID substrings on Windows) into one
// interface-class triple. ok is false if any of the three fails to parse,
// so callers skip the whole interface rather than recording a partial/wrong
// one -- shared by list_sysfs_linux.go's readInterfaceClasses and
// list_setupapi_windows.go's parseInterfaceClasses, the only two real
// differences between those two being how class/sub/proto strings are
// obtained in the first place.
func parseHexTriple(class, sub, proto string) (triple [3]uint8, ok bool) {
	c, err1 := strconv.ParseUint(class, 16, 8)
	s, err2 := strconv.ParseUint(sub, 16, 8)
	p, err3 := strconv.ParseUint(proto, 16, 8)
	if err1 != nil || err2 != nil || err3 != nil {
		return [3]uint8{}, false
	}
	return [3]uint8{uint8(c), uint8(s), uint8(p)}, true
}

func ListLocal() ([]models.USBPassthroughDevice, error) {
	switch runtime.GOOS {
	case "linux":
		return listSysfs()
	case "darwin":
		return listHIDDarwin()
	case "android":
		return listUSBAndroid()
	case "windows":
		return listSetupAPI()
	}
	return nil, fmt.Errorf("usb passthrough listing not supported on %s", runtime.GOOS)
}

// StableUSBIPBusID maps a Windows instance id to a Linux-style busid (N-M)
// that usbip-win2 VHCI expects in PLUGIN_HARDWARE.
func StableUSBIPBusID(instanceID string) string {
	h := fnv.New32a()
	_, _ = h.Write([]byte(instanceID))
	v := h.Sum32()
	bus := (v % 9) + 1
	port := (v/9)%200 + 1
	return strconv.FormatUint(uint64(bus), 10) + "-" + strconv.FormatUint(uint64(port), 10)
}

// AttachOptions configures the AES attach to the agent's control plane.
// Attach/StopAttach live in usbaes_attach.go (Go, linux/windows) or
// usbaes_attach_stub.go (everywhere else) — see those files.
type AttachOptions struct {
	AgentAddr       string
	Secret          string
	InstanceID      string // Windows SetupAPI id (used to derive USBIPBusID if unset)
	USBIPBusID      string // Linux-style id advertised by our export server
	VID             string // hex, e.g. "0781" — from models.USBPassthroughDevice
	PID             string // hex, e.g. "55A9"
	ExportService   string // default 3240
	AllowUnlicensed bool   // unused client-side: the entitlement gate is on the agent
	// Dialer, if set, replaces the plain net.Dialer.Dial Attach otherwise
	// uses to reach AgentAddr -- e.g. a Tailscale tsnet dialer, needed
	// because AgentAddr can be a 100.x tailnet IP that kernel BSD sockets
	// can't route to on their own (same constraint moonlight_tsnet_proxy.go
	// works around for the video/control streams). nil means AgentAddr is
	// reachable directly (LAN/localhost).
	Dialer func(ctx context.Context, network, addr string) (net.Conn, error)
}
