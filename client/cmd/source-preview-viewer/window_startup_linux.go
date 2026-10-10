//go:build linux && !android && cgo

package main

// Linux's existing X11 pixel acceptance remains the platform proof. The new
// owned HWND prerequisite is Windows-specific.
func previewWindowFailure(any) string { return "" }
