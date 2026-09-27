//go:build windows

package benchvideo

import (
	"errors"
	"fmt"
	"os/exec"

	"usbridge_agent/internal/sessionlaunch"
)

type cmdProcess struct{ cmd *exec.Cmd }

func (c cmdProcess) Kill() error { return c.cmd.Process.Kill() }
func (c cmdProcess) Wait() error { return c.cmd.Wait() }

type sessionProcess struct{ h *sessionlaunch.Handle }

func (s sessionProcess) Kill() error { return s.h.Kill() }
func (s sessionProcess) Wait() error {
	code, err := s.h.Wait()
	s.h.Close()
	if err != nil {
		return err
	}
	if code != 0 {
		return fmt.Errorf("exit code %d", code)
	}
	return nil
}

// launch puts the player on the interactive desktop. The agent normally
// runs as a Session 0 service, where a window is invisible to both DXGI
// capture paths, so it goes through sessionlaunch first; an agent started
// by hand in the user's session lacks SeTcbPrivilege for that and just
// starts the player directly.
func launch(path string, args []string) (process, error) {
	h, err := sessionlaunch.LaunchInActiveSession(path, args, "", nil, nil, nil)
	if err == nil {
		return sessionProcess{h}, nil
	}
	if errors.Is(err, sessionlaunch.ErrNoActiveSession) {
		return nil, err
	}
	cmd := exec.Command(path, args...)
	if cerr := cmd.Start(); cerr != nil {
		return nil, fmt.Errorf("%v (session launch: %v)", cerr, err)
	}
	return cmdProcess{cmd}, nil
}
