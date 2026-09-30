//go:build darwin

package netutil

import (
	"fmt"
	"os"
	"os/exec"
	"os/user"
	"strings"
)

// AWDL (Apple Wireless Direct Link) is the mesh protocol behind AirDrop,
// Handoff, Sidecar, and Instant Hotspot. It shares the same Wi-Fi radio as
// the regular network connection and periodically forces channel-hopping
// scans even when idle, which measurably hurts latency/throughput for a
// concurrent Wi-Fi streaming session -- confirmed live: `sudo ifconfig
// awdl0 down` visibly improves a streaming connection's stability. This
// file lets the agent do that itself instead of the user having to run a
// manual `while true; do sudo ifconfig awdl0 down; sleep 1; done` loop in
// a terminal.
//
// Bringing awdl0 down/up always requires root -- there is no unprivileged
// path, on this or any macOS version, since it's a genuine network
// interface state change. What CAN be avoided is prompting for a password
// on every single toggle: a narrow `NOPASSWD` sudoers.d rule, scoped to
// exactly these two commands for exactly the current user, is installed
// once (with one admin-password prompt via osascript) and then every
// later `sudo -n ifconfig awdl0 down/up` succeeds without prompting.
//
// A privileged LaunchDaemon helper (SMAppService + XPC) is Apple's more
// "canonical" mechanism for this, but this agent is a Go/Fyne app with no
// native XPC -- that route means a second signed daemon target and a
// hand-rolled IPC protocol over a Unix socket, which is disproportionate
// for toggling one interface with two fixed commands. The sudoers.d rule
// is the standard lighter-weight alternative for exactly this shape of
// problem and is what's implemented here.

// sudoersPath is where the NOPASSWD rule lives. A dedicated file under
// sudoers.d rather than editing /etc/sudoers directly, so it's trivial to
// find, replace, or remove independently of the rest of the system's sudo
// configuration.
const sudoersPath = "/etc/sudoers.d/usbridge-awdl"

// ifconfigPath is hardcoded to its standard absolute path (rather than
// resolved via PATH) deliberately: sudoers NOPASSWD rules should always
// name an exact executable path, never a bare command name, so nothing on
// $PATH could be substituted to run as root under this grant.
const ifconfigPath = "/sbin/ifconfig"

func awdlUsername() (string, error) {
	u, err := user.Current()
	if err != nil {
		return "", fmt.Errorf("looking up current user: %w", err)
	}
	return u.Username, nil
}

// awdlSudoersRuleContent is the exact line installed at sudoersPath: only
// the two specific `ifconfig awdl0 down/up` invocations, nothing broader.
func awdlSudoersRuleContent(username string) string {
	return fmt.Sprintf("%s ALL=(root) NOPASSWD: %s awdl0 down, %s awdl0 up\n", username, ifconfigPath, ifconfigPath)
}

// AWDLSudoersPreview returns the exact rule EnsureAWDLSudoRule would
// install, for showing the user before the admin-password prompt fires --
// mirrors permissions.Service.ClipboardInstallPreview's "show the command
// before it runs, not after clicking Install" pattern. Empty if the
// current user can't be determined.
func AWDLSudoersPreview() string {
	username, err := awdlUsername()
	if err != nil {
		return ""
	}
	return sudoersPath + ":\n" + awdlSudoersRuleContent(username)
}

// AWDLSudoRuleInstalled reports whether sudo would currently let the
// current user run both awdl0 commands without a password prompt. Checked
// via `sudo -n -l <cmd>` (list/validate, per sudo(8) -- doesn't execute
// the command, just reports whether it's permitted) rather than reading
// sudoersPath directly: that file is installed root:wheel 0440, which an
// unprivileged agent process can't read back regardless of whether the
// rule is actually in effect, so a direct read would misreport "not
// installed" every time.
func AWDLSudoRuleInstalled() bool {
	for _, state := range []string{"down", "up"} {
		cmd := exec.Command("sudo", "-n", "-l", ifconfigPath, "awdl0", state)
		if err := cmd.Run(); err != nil {
			return false
		}
	}
	return true
}

// EnsureAWDLSudoRule installs the NOPASSWD sudoers.d rule if it isn't
// already in effect, prompting once for an administrator password via
// osascript. No-op (no prompt) if the rule is already installed.
func EnsureAWDLSudoRule() error {
	if AWDLSudoRuleInstalled() {
		return nil
	}
	username, err := awdlUsername()
	if err != nil {
		return err
	}

	tmp, err := os.CreateTemp("", "usbridge-awdl-sudoers-*")
	if err != nil {
		return fmt.Errorf("creating temp sudoers file: %w", err)
	}
	tmpPath := tmp.Name()
	defer os.Remove(tmpPath)
	if _, err := tmp.WriteString(awdlSudoersRuleContent(username)); err != nil {
		tmp.Close()
		return fmt.Errorf("writing temp sudoers file: %w", err)
	}
	if err := tmp.Close(); err != nil {
		return fmt.Errorf("closing temp sudoers file: %w", err)
	}

	// visudo -cf validates syntax before install ever touches the real
	// sudoers.d directory -- a malformed file there can lock sudo up
	// entirely, so this check runs first and the whole chain aborts (via
	// &&) if it fails.
	shellCmd := fmt.Sprintf("visudo -cf %s && install -m 0440 -o root -g wheel %s %s",
		shellSingleQuote(tmpPath), shellSingleQuote(tmpPath), shellSingleQuote(sudoersPath))
	appleScript := fmt.Sprintf(
		`do shell script %s with administrator privileges with prompt %s`,
		appleScriptQuote(shellCmd),
		appleScriptQuote("USBridge needs administrator access once to allow disabling AWDL (AirDrop/Handoff's Wi-Fi mesh) during streaming, which reduces Wi-Fi interference. This installs a narrow permission for exactly that, so you won't be asked again."),
	)

	cmd := exec.Command("osascript", "-e", appleScript)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("installing AWDL sudoers rule: %w (%s)", err, strings.TrimSpace(string(out)))
	}
	if !AWDLSudoRuleInstalled() {
		return fmt.Errorf("AWDL sudoers rule installed but verification still fails")
	}
	return nil
}

// SetAWDLDown brings awdl0 down (true) or back up (false) via a
// non-interactive sudo call. Fails fast (no password prompt, no hang) if
// EnsureAWDLSudoRule hasn't been run or the rule was since removed --
// callers should treat an error here as "AWDL control unavailable right
// now" rather than retrying in a loop.
func SetAWDLDown(down bool) error {
	state := "up"
	if down {
		state = "down"
	}
	cmd := exec.Command("sudo", "-n", ifconfigPath, "awdl0", state)
	out, err := cmd.CombinedOutput()
	if err != nil {
		return fmt.Errorf("sudo -n %s awdl0 %s: %w (%s)", ifconfigPath, state, err, strings.TrimSpace(string(out)))
	}
	return nil
}

// shellSingleQuote wraps s in single quotes for safe embedding in a shell
// command line, escaping any single quotes already in s.
func shellSingleQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}

// appleScriptQuote wraps s in double quotes for safe embedding as an
// AppleScript string literal, escaping backslashes and double quotes.
func appleScriptQuote(s string) string {
	s = strings.ReplaceAll(s, `\`, `\\`)
	s = strings.ReplaceAll(s, `"`, `\"`)
	return `"` + s + `"`
}
