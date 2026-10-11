//go:build !windows

package previeweligibility

// Observe is Windows-specific and fails closed on every other platform.
func Observe() Snapshot { return Snapshot{} }
