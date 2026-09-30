//go:build linux && !android

package gui

import (
	"errors"
	"os/exec"
	"strings"
)

func nativeSaveAvailable() bool { return true }

func nativeSaveFile(title, defaultName string) (string, error) {
	cmd := exec.Command(
		"zenity",
		"--file-selection",
		"--save",
		"--confirm-overwrite",
		"--title="+title,
		"--filename="+defaultName,
		"--file-filter=ZIP archive | *.zip",
		"--file-filter=All files | *",
	)
	out, err := cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	if !execNotFound(err) {
		if _, ok := err.(*exec.ExitError); ok {
			return "", nil
		}
		return "", err
	}

	cmd = exec.Command("kdialog", "--getsavefilename", defaultName, "*.zip | ZIP archive", "--title", title)
	out, err = cmd.Output()
	if err == nil {
		return strings.TrimSpace(string(out)), nil
	}
	if execNotFound(err) {
		return "", err
	}
	if _, ok := err.(*exec.ExitError); ok {
		return "", nil
	}
	return "", err
}

func execNotFound(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, exec.ErrNotFound) {
		return true
	}
	var ee *exec.Error
	return errors.As(err, &ee) && errors.Is(ee.Err, exec.ErrNotFound)
}
