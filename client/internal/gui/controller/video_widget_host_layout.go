package controller

import (
	"errors"
	"strings"
	"sync"
	"time"

	"github.com/sirupsen/logrus"

	"usbridge-client/internal/api"
	"usbridge-client/internal/input"
	"usbridge-client/internal/service"
)

// Typing non-Latin text into Sunshine on a Linux host.
//
// In text mode a Cyrillic character can't go as Moonlight UTF-8 text to
// Sunshine on Linux: its unicode() "types" an IBus Ctrl+Shift+U <hex>
// sequence that only GTK apps compose, so KDE and most apps print the hex
// digits instead ("и" → "438"). A keypress is only a key position, so the
// letter it produces depends on the host's layout. Instead the client asks
// the agent to switch the host to the matching layout (KDE Wayland, see
// agent/internal/input/layout_linux.go) and then presses the key the
// character sits on in that layout -- and switches back before Latin text.
// RustShine does the same switch on the host by itself for UTF-8 text, and
// Windows/macOS hosts inject Unicode natively, so this only runs for
// Sunshine on Linux.

// hostLayoutTTL: how long a layout this client set is trusted before it is
// re-asserted (the host user may have switched it by hand meanwhile).
const hostLayoutTTL = 2 * time.Second

type hostLayoutState struct {
	mu          sync.Mutex
	lang        string // "ru", "en", "" = unknown
	setAt       time.Time
	unsupported bool
}

// typesViaHostLayout reports whether text-mode typing must drive the host
// layout itself (Sunshine on a Linux agent).
func (vw *VideoWidget) typesViaHostLayout() bool {
	proto := strings.ToLower(strings.TrimSpace(vw.agentProtocol))
	if proto != "opensource" && proto != "open source" && proto != "sunshine" {
		return false
	}
	return strings.Contains(strings.ToLower(vw.agentOS), "linux")
}

// ensureHostLayout switches the host to lang unless this client already
// did so recently. Runs on the send worker, so it completes before the
// keystroke queued after it. Returns false when the host can't switch.
func (vw *VideoWidget) ensureHostLayout(lang string) bool {
	h := &vw.hostLayout
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.unsupported {
		return false
	}
	if h.lang == lang && time.Since(h.setAt) < hostLayoutTTL {
		h.setAt = time.Now()
		return true
	}
	client := vw.usbClient
	if client == nil {
		return false
	}
	layout, err := client.SetHostKeyboardLayout(lang)
	if err != nil {
		if errors.Is(err, api.ErrHostLayoutUnsupported) {
			logrus.Infof("⌨️ [host-layout] host can't switch keyboard layout; non-Latin text falls back to UTF-8")
			h.unsupported = true
		} else {
			logrus.Warnf("⌨️ [host-layout] switch to %s: %v", lang, err)
		}
		h.lang = ""
		return false
	}
	logrus.Debugf("⌨️ [host-layout] host layout %s", layout)
	h.lang, h.setAt = lang, time.Now()
	return true
}

// resetHostLayout forgets what the client set (new session / backend).
func (vw *VideoWidget) resetHostLayout() {
	h := &vw.hostLayout
	h.mu.Lock()
	h.lang, h.unsupported = "", false
	h.mu.Unlock()
}

// sendRuneViaHostLayout types r on a Sunshine/Linux host by layout switch
// plus key position. Returns false when r has no such mapping.
func (vw *VideoWidget) sendRuneViaHostLayout(r rune) bool {
	mi := vw.moonlightInput()
	if mi == nil {
		return false
	}
	lang := "en"
	if r > 127 {
		lang = "ru"
		if _, ok := input.RussianLayoutRuneToLatin(r); !ok {
			return false
		}
	}
	hidCode, hidMods := input.GetRuneKeyCodeWithModifiers(r)
	if hidCode == 0 {
		return false
	}
	vk := hidKeyToVK(hidCode)
	mods := widgetToMoonlightModifiers(hidMods)
	vw.enqueueSend(func() {
		if !vw.ensureHostLayout(lang) && lang != "en" {
			// No switching: UTF-8 is the only remaining way to get the
			// character itself across.
			mi.SendMoonlightUtf8Text(string(r))
			return
		}
		mi.SendMoonlightKey(vk, service.LiKeyActionDown, mods)
		mi.SendMoonlightKey(vk, service.LiKeyActionUp, mods)
	})
	return true
}
