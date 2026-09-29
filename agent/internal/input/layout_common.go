package input

import "errors"

var (
	// ErrLayoutUnsupported: this host can't switch its keyboard layout
	// remotely (non-KDE Linux session, Windows, macOS).
	ErrLayoutUnsupported = errors.New("keyboard layout switching is not supported on this host")
	// ErrLayoutNotConfigured: the requested layout isn't one of the host's.
	ErrLayoutNotConfigured = errors.New("keyboard layout not configured")
)
