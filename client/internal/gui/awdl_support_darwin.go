//go:build darwin && !ios

package gui

// awdlSupported is true only for desktop macOS builds -- iOS also reports
// runtime.GOOS=="darwin" but has no ifconfig/sudo, hence the build-tag
// split (matching internal/usbpass/access_darwin.go's same "darwin &&
// !ios" convention) instead of a plain runtime.GOOS check.
func awdlSupported() bool { return true }
