//go:build windows

package previewchild

import (
	"context"
	"errors"
	"io"
	"path/filepath"
	"strings"
	"time"

	"usbridge_agent/internal/previewprocess"
)

type Process struct {
	Stdin      io.WriteCloser
	Stdout     io.ReadCloser
	owner      *previewprocess.Process
	stderrDone chan struct{}
}

func Start(ctx context.Context, spec Spec) (*Process, error) {
	owner, err := previewprocess.Start(ctx, previewprocess.Spec{
		Path: filepath.Clean(spec.Path), Args: spec.Args, Env: explicitEnvironment(spec.Env),
		Dir: filepath.Dir(filepath.Clean(spec.Path)),
	})
	if err != nil {
		return nil, errors.Join(ErrStart, err)
	}
	p := &Process{Stdin: owner.Stdin, Stdout: owner.Stdout, owner: owner, stderrDone: make(chan struct{})}
	// The owner has its own root waiter. Neither root crash observation nor
	// descendant retirement depends on this drain or on the protocol scanner.
	go func() {
		defer close(p.stderrDone)
		defer owner.Stderr.Close()
		_, _ = io.Copy(io.Discard, owner.Stderr)
	}()
	return p, nil
}

func (p *Process) Wait() error {
	_, err := p.owner.Wait()
	return errors.Join(err, Join(p.stderrDone))
}

func (p *Process) Abort() { _, _ = p.owner.Stop(0) }

func (p *Process) Stop(done <-chan struct{}, grace time.Duration) error {
	grace = max(0, min(grace, previewprocess.MaxStopGrace))
	deadline := time.Now().Add(grace + previewprocess.CleanupTimeout)
	_, err := p.owner.Stop(grace)
	// Stdout is closed by the scanner; forced owner cleanup closes both native
	// readers first. Natural completion must retain buffered terminal events.
	return errors.Join(err, joinUntil(deadline, p.stderrDone, done))
}

// Windows can inherit hidden =C:= drive working-directory entries. The owner
// intentionally rejects pseudo-variables; an explicit local Dir makes them
// unnecessary. Leave every ordinary variable under the caller's own policy.
func explicitEnvironment(env []string) []string {
	result := make([]string, 0, len(env))
	for _, entry := range env {
		if !strings.HasPrefix(entry, "=") {
			result = append(result, entry)
		}
	}
	return result
}
