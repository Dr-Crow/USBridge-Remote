//go:build !windows

package benchvideo

import "os/exec"

type cmdProcess struct{ cmd *exec.Cmd }

func (c cmdProcess) Kill() error { return c.cmd.Process.Kill() }
func (c cmdProcess) Wait() error { return c.cmd.Wait() }
func (c cmdProcess) Pid() int    { return c.cmd.Process.Pid }

// launch starts the player with the agent's own environment: the Linux
// user unit and the macOS LaunchAgent both run inside the desktop session,
// so DISPLAY/WAYLAND_DISPLAY are already the ones the streamers capture.
func launch(path string, args []string) (process, error) {
	cmd := exec.Command(path, args...)
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	return cmdProcess{cmd}, nil
}
