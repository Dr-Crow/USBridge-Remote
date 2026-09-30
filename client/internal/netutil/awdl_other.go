//go:build !darwin || ios

package netutil

import "fmt"

// AWDL is a macOS-desktop-only concept (Apple Wireless Direct Link) -- see
// awdl_darwin.go for the real implementation. Every other platform
// (including iOS, which reports GOOS=darwin too but has no ifconfig/sudo)
// stubs this out so callers don't need their own build-tag branching.

func EnsureAWDLSudoRule() error {
	return fmt.Errorf("AWDL control is only supported on desktop macOS")
}

func SetAWDLDown(down bool) error {
	return fmt.Errorf("AWDL control is only supported on desktop macOS")
}

func AWDLSudoRuleInstalled() bool { return false }

func AWDLSudoersPreview() string { return "" }
