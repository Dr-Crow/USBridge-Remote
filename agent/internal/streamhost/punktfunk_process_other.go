//go:build !windows

package streamhost

import "os/exec"

// afterStartPunktfunk is a no-op off Windows. On Linux, configureProcess's
// Pdeathsig already gives an OS-enforced kill-on-parent-death guarantee
// (sunshine_process_linux.go, reused as-is since it only takes *exec.Cmd).
// macOS is unreachable here: punktfunkBackend.binaryPath always returns ""
// on darwin (no macOS host build), so Start() never gets far enough to call
// this.
func afterStartPunktfunk(cmd *exec.Cmd) {}

// repairPunktfunkConfigDir is Windows-only: see punktfunk_process_windows.go.
func repairPunktfunkConfigDir(string) {}
