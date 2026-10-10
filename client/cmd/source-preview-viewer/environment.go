package main

import (
	"errors"
	"os"
	"strings"
)

// clearPreviewEnvironment removes every inherited USBridge diagnostic/config
// switch, including future ones. This dedicated child has no environment-based
// USBridge configuration. DISPLAY/XAUTHORITY and other ordinary runtime values
// are retained. os.Unsetenv also updates the C environment in a cgo build, so
// the decoder's getenv() frame-dump and skip-decode switches are cleared too.
// Call before constructing Fyne or starting native decoding; the parent also
// removes this prefix before exec, before imported packages can initialize.
func clearPreviewEnvironment() error {
	return clearPreviewEnvironmentWith(os.Environ(), os.Unsetenv)
}

func clearPreviewEnvironmentWith(environment []string, unset func(string) error) error {
	for _, entry := range environment {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(name), "USBRIDGE_") {
			if err := unset(name); err != nil {
				return errors.New("could not clear inherited preview configuration")
			}
		}
	}
	return nil
}

// validLocalDisplay accepts only a literal local X11 display, :N or :N.S.
// Hostname, TCP, protocol-qualified and abstract environment-derived routes
// are not supported by the initial same-host preview.
func validLocalDisplay(value string) bool {
	if len(value) < 2 || len(value) > 32 || value[0] != ':' {
		return false
	}
	display, screen, hasScreen := strings.Cut(value[1:], ".")
	digits := func(s string) bool {
		if s == "" {
			return false
		}
		for _, c := range s {
			if c < '0' || c > '9' {
				return false
			}
		}
		return true
	}
	return digits(display) && (!hasScreen || digits(screen))
}
