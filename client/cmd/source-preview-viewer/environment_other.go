//go:build !windows || !cgo

package main

// Unix cgo synchronizes os.Unsetenv with libc; non-cgo builds have no CRT.
func clearPreviewNativeEnvironment() error { return nil }
