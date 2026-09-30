//go:build !darwin || ios

package gui

// awdlSupported: AWDL doesn't exist off desktop macOS -- see
// awdl_support_darwin.go.
func awdlSupported() bool { return false }
