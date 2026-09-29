//go:build windows

package usbpass

import (
	"os/exec"
	"syscall"
)

func hideBrokerWindow(cmd *exec.Cmd) {
	if cmd.SysProcAttr == nil {
		cmd.SysProcAttr = &syscall.SysProcAttr{}
	}
	cmd.SysProcAttr.HideWindow = true
	cmd.SysProcAttr.CreationFlags = 0x08000000 // CREATE_NO_WINDOW
}

// killBrokersByName force-kills every running process named name (the
// broker image) and reports whether taskkill found any.
func killBrokersByName(name string) bool {
	cmd := exec.Command("taskkill", "/F", "/IM", name)
	hideBrokerWindow(cmd)
	return cmd.Run() == nil
}
