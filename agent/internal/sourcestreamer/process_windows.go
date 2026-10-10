//go:build windows

package sourcestreamer

import (
	"os/exec"
	"syscall"
)

// Pipe-only supervision must not allocate a hidden console host. No token,
// desktop, job-breakaway or permission changes are requested. The descriptor
// remains confined to the existing private stdin handle.
func configurePipeProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{CreationFlags: 0x8} // DETACHED_PROCESS
}
