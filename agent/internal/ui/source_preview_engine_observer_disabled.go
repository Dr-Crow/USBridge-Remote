//go:build !linux || !source_preview_engine_acceptance

package ui

// Ordinary builds never observe or export native UI/engine acceptance metadata.
func observeSourcePreviewEngine(*Window) func() { return func() {} }
