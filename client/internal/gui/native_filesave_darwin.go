//go:build darwin

package gui

import (
	"fmt"
	"os/exec"
	"strings"
)

func nativeSaveAvailable() bool { return true }

func nativeSaveFile(title, defaultName string) (string, error) {
	script := fmt.Sprintf(
		`POSIX path of (choose file name with prompt %q default name %q)`,
		title, defaultName,
	)
	cmd := exec.Command("osascript", "-e", script)
	out, err := cmd.Output()
	if err != nil {
		// User cancelled: osascript exits non-zero with no path.
		if _, ok := err.(*exec.ExitError); ok {
			return "", nil
		}
		return "", err
	}
	return strings.TrimSpace(string(out)), nil
}
