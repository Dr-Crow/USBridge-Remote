//go:build !windows

package sourcestreamer

import "os/exec"

func configurePipeProcess(_ *exec.Cmd) {}
