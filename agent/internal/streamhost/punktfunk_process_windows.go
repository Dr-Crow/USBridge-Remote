//go:build windows

package streamhost

import (
	"log"
	"os/exec"

	"golang.org/x/sys/windows"
)

// afterStartPunktfunk assigns the freshly-started punktfunk-host process to
// the same kill-on-job-close Job Object sunshine_process_windows.go's own
// afterStart uses, so Windows itself terminates punktfunk-host the instant
// the agent process ends for any reason. killOnCloseJob is package-level and
// backend-agnostic already (sunshine_process_windows.go), so it's reused
// directly rather than duplicated.
func afterStartPunktfunk(cmd *exec.Cmd) {
	job := killOnCloseJob()
	if job == 0 {
		return
	}
	procHandle, err := windows.OpenProcess(windows.PROCESS_SET_QUOTA|windows.PROCESS_TERMINATE, false, uint32(cmd.Process.Pid))
	if err != nil {
		log.Printf("[punktfunk] OpenProcess failed, punktfunk-host may survive an agent crash: %v", err)
		return
	}
	defer windows.CloseHandle(procHandle)
	if err := windows.AssignProcessToJobObject(job, procHandle); err != nil {
		log.Printf("[punktfunk] AssignProcessToJobObject failed, punktfunk-host may survive an agent crash: %v", err)
	}
}
