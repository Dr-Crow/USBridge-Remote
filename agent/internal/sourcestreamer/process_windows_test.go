//go:build windows

package sourcestreamer

import (
	"os/exec"
	"testing"
)

func TestWindowsPipeProcessUsesNoConsoleOrBreakaway(t *testing.T) {
	cmd := exec.Command("unused.exe")
	configurePipeProcess(cmd)
	if cmd.SysProcAttr == nil || cmd.SysProcAttr.CreationFlags != 0x8 || cmd.SysProcAttr.HideWindow || cmd.SysProcAttr.Token != 0 || len(cmd.SysProcAttr.AdditionalInheritedHandles) != 0 {
		t.Fatal("pipe-only process policy changed")
	}
}
