//go:build !windows

package previewchild

import (
	"context"
	"errors"
	"io"
	"os/exec"
	"time"
)

type Process struct {
	Stdin  io.WriteCloser
	Stdout io.ReadCloser
	cmd    *exec.Cmd
}

func Start(ctx context.Context, spec Spec) (*Process, error) {
	cmd := exec.CommandContext(ctx, spec.Path, spec.Args...)
	cmd.Env = spec.Env
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, ErrStart
	}
	if spec.CloseStdinOnCancel {
		cmd.Cancel = func() error { _ = stdin.Close(); return nil }
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		_ = stdin.Close()
		return nil, ErrStart
	}
	if err = cmd.Start(); err != nil {
		_ = stdin.Close()
		_ = stdout.Close()
		return nil, ErrStart
	}
	return &Process{Stdin: stdin, Stdout: stdout, cmd: cmd}, nil
}

func (p *Process) Wait() error {
	if p.cmd.Wait() != nil {
		return errors.New("preview child exited unsuccessfully")
	}
	return nil
}
func (p *Process) Abort() { _ = p.cmd.Process.Kill() }
func (p *Process) Stop(done <-chan struct{}, grace time.Duration) error {
	// Preserve the existing Unix EOF-then-root-kill behavior. No Windows-style
	// containment guarantee is implied for the Unix implementation.
	_ = p.Stdin.Close()
	timer := time.NewTimer(grace)
	defer timer.Stop()
	select {
	case <-done:
		return nil
	case <-timer.C:
		p.Abort()
	}
	return Join(done)
}
