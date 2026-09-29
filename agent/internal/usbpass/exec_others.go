//go:build !windows

package usbpass

import "os/exec"

func hideBrokerWindow(cmd *exec.Cmd) {}

// killBrokersByName force-kills every running agent-role broker named name
// and reports whether any was found. Matches the command line (-f), not -x:
// Linux truncates the process name to 15 chars, so "usbridge-usb-broker"
// would never match exactly.
func killBrokersByName(name string) bool {
	return exec.Command("pkill", "-KILL", "-f", name+" --role agent").Run() == nil
}
