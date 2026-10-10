//go:build linux && source_preview_acceptance

// This is a CI-only native shell for the shipped agent Window, settings, dialog
// and tray callbacks. It never constructs the enrollment/service App engine.
package main

import (
	"fmt"
	"os"
	"usbridge_agent/internal/ui"
)

func main() {
	if err := ui.RunSourcePreviewAcceptance(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}
