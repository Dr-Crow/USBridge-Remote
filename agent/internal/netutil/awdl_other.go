//go:build !darwin

package netutil

import "fmt"

// AWDL is a macOS-only concept (Apple Wireless Direct Link) -- see
// awdl_darwin.go for the real implementation. Every other platform stubs
// this out so callers (e.g. the app-level AWDL watchdog) don't need their
// own build-tag branching.

func EnsureAWDLSudoRule() error {
	return fmt.Errorf("AWDL control is only supported on macOS")
}

func SetAWDLDown(down bool) error {
	return fmt.Errorf("AWDL control is only supported on macOS")
}

func AWDLSudoRuleInstalled() bool { return false }

func AWDLSudoersPreview() string { return "" }
