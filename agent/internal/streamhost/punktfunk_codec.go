package streamhost

import (
	"encoding/json"
	"net/http"
)

// punktfunkRuntimeStatus is the subset of punktfunk-host's RuntimeStatus
// schema (GET /api/v1/status) this backend actually reads. json.Decode
// ignores the many fields not listed here (games, sessions, audio,
// display, ...) -- confirmed shape from api/openapi.json's RuntimeStatus
// component.
type punktfunkRuntimeStatus struct {
	ActiveSessions int `json:"active_sessions"`
}

// SessionActive reports whether a Moonlight client is currently mid-stream
// on Punktfunk's GameStream plane, via its own live RuntimeStatus.active_sessions
// (the app's real state, not a log scrape) -- same precedent as
// rustshineBackend.SessionActive (rustshine_codec.go) and the fixed
// sunshineBackend.SessionActive (session_active.go).
func (b *punktfunkBackend) SessionActive() bool {
	b.mu.Lock()
	adminPort, token := b.adminPort, b.token
	b.mu.Unlock()
	if adminPort <= 0 {
		return false
	}
	resp, err := punktfunkRequest(http.MethodGet, adminPort, "/status", token, nil)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return false
	}
	var status punktfunkRuntimeStatus
	if json.NewDecoder(resp.Body).Decode(&status) != nil {
		return false
	}
	return status.ActiveSessions > 0
}

// CurrentVideoCodec has no confirmed Punktfunk equivalent yet (RuntimeStatus
// was only read for active_sessions this pass -- see
// agent/docs/PUNKTFUNK_BACKEND_TODO.md). "" means unknown, same as any other
// backend's best-effort hint when it has nothing to report.
func (b *punktfunkBackend) CurrentVideoCodec() string { return "" }

// SupportedVideoCodecs has no confirmed Punktfunk endpoint yet (not among
// the 96 routes read from api/openapi.json this pass). Empty means
// "unknown" -- callers already treat that as "don't show a codec picker".
func (b *punktfunkBackend) SupportedVideoCodecs(adminPort int) []string { return nil }

// Color444Status: no confirmed Punktfunk concept (RustShine Pro-only color
// upgrade) -- always (false, false), same contract CodecProbe documents for
// Sunshine.
func (b *punktfunkBackend) Color444Status() (active bool, available bool) { return false, false }

// HdrStatus: no confirmed Punktfunk admin-API surface for this yet (distinct
// from VirtualDisplaySupported, which IS confirmed via pf-vdisplay) --
// always (false, false) until a real endpoint is found.
func (b *punktfunkBackend) HdrStatus() (active bool, available bool) { return false, false }

// VirtualDisplaySupported is true: Punktfunk's pf-vdisplay crate creates a
// virtual output per session on every supported compositor (KWin, Mutter,
// Hyprland, wlroots, gamescope) and via the Windows IddCx driver -- confirmed
// directly from docs-site/content/docs/developers/architecture.md ("The host
// never scales a virtual display. Each session gets an output at the
// client's WxH@Hz").
func (b *punktfunkBackend) VirtualDisplaySupported() bool { return true }

// RawHIDSupported is true only for a punktfunk-host built with the USBridge
// patch, which hands LiSendRawHidEvent to the USB broker (see
// punktfunkBrokerEnv). Stock Punktfunk has no handler for it. Whether a
// tablet is really built is the broker's call (license, and a broker new
// enough to know hid_stream): the host only sets LI_FF_USBRIDGE_RAW_HID
// when the broker says yes, and the client checks that flag as well.
func (b *punktfunkBackend) RawHIDSupported() bool { return punktfunkHasBridge(b.binaryPath()) }
