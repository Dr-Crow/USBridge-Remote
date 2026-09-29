//go:build linux

package input

import (
	"fmt"
	"strings"
	"sync"

	"github.com/godbus/dbus/v5"
)

// Host keyboard layout switching through kwin_wayland's own
// org.kde.keyboard D-Bus service -- the one mechanism that changes which
// layout a native Wayland client interprets our uinput key presses under
// (rust-shine's crates/enet-input/src/kde_layout.rs documents the ones
// that don't work). The client uses it to type non-Latin text through
// Sunshine, whose Linux Unicode injection (an IBus Ctrl+Shift+U hex
// sequence) only works inside GTK apps and prints the hex digits
// everywhere else.

const (
	kdeKeyboardService = "org.kde.keyboard"
	kdeKeyboardPath    = "/Layouts"
	kdeKeyboardIface   = "org.kde.KeyboardLayouts"
)

var kdeLayoutMu sync.Mutex

type kdeLayoutEntry struct {
	Short, Variant, Description string
}

func kdeLayouts() (dbus.BusObject, []kdeLayoutEntry, uint32, error) {
	conn, err := dbus.SessionBus()
	if err != nil {
		return nil, nil, 0, fmt.Errorf("%w: no session bus: %v", ErrLayoutUnsupported, err)
	}
	obj := conn.Object(kdeKeyboardService, kdeKeyboardPath)
	var list []kdeLayoutEntry
	if err := obj.Call(kdeKeyboardIface+".getLayoutsList", 0).Store(&list); err != nil {
		return nil, nil, 0, fmt.Errorf("%w: %v", ErrLayoutUnsupported, err)
	}
	var cur uint32
	if err := obj.Call(kdeKeyboardIface+".getLayout", 0).Store(&cur); err != nil {
		return nil, nil, 0, fmt.Errorf("%w: %v", ErrLayoutUnsupported, err)
	}
	return obj, list, cur, nil
}

// CurrentKeyboardLayout returns the short code ("us", "ru", ...) of the
// layout the host is typing with right now.
func CurrentKeyboardLayout() (string, error) {
	kdeLayoutMu.Lock()
	defer kdeLayoutMu.Unlock()
	_, list, cur, err := kdeLayouts()
	if err != nil {
		return "", err
	}
	if int(cur) >= len(list) {
		return "", fmt.Errorf("layout index %d out of range", cur)
	}
	return list[cur].Short, nil
}

// SetKeyboardLayout makes the first configured layout matching lang the
// active one and returns its short code. lang is a layout short code or a
// language: "en" matches us/gb/..., anything else matches its own code.
func SetKeyboardLayout(lang string) (string, error) {
	kdeLayoutMu.Lock()
	defer kdeLayoutMu.Unlock()
	obj, list, cur, err := kdeLayouts()
	if err != nil {
		return "", err
	}
	want := layoutMatcher(lang)
	if int(cur) < len(list) && want(list[cur].Short) {
		return list[cur].Short, nil
	}
	for i, l := range list {
		if want(l.Short) {
			var ok bool
			if err := obj.Call(kdeKeyboardIface+".setLayout", 0, uint32(i)).Store(&ok); err != nil {
				return "", err
			}
			if !ok {
				return "", fmt.Errorf("kwin refused layout %q", l.Short)
			}
			return l.Short, nil
		}
	}
	return "", fmt.Errorf("%w: no %q layout configured on the host", ErrLayoutNotConfigured, lang)
}

func layoutMatcher(lang string) func(string) bool {
	lang = strings.ToLower(strings.TrimSpace(lang))
	if lang == "en" {
		return func(s string) bool {
			switch strings.ToLower(s) {
			case "us", "gb", "en", "au", "ca":
				return true
			}
			return false
		}
	}
	return func(s string) bool { return strings.EqualFold(s, lang) }
}
