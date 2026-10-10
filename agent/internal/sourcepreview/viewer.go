package sourcepreview

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"io"
	"os"
	"os/exec"
	"strings"
	"sync"
	"time"

	"usbridge_agent/internal/componentjson"
	"usbridge_agent/internal/localcomponents"
)

func prepareViewer(ctx context.Context, o localcomponents.Options) (string, error) {
	r, err := localcomponents.Resolve(ctx, o, "source-preview-viewer")
	if err != nil {
		return "", err
	}
	if r.Profile != Profile {
		return "", errors.New("unexpected preview viewer profile")
	}
	if err := localcomponents.VerifyPrepared(r.Binary); err != nil {
		return "", err
	}
	return r.Binary, nil
}

type viewerEvent struct {
	SchemaVersion int    `json:"schema_version"`
	Event         string `json:"event"`
	SessionID     string `json:"session_id"`
	Reason        string `json:"reason,omitempty"`
}
type viewerProcess struct {
	cmd        *exec.Cmd
	stdin      io.WriteCloser
	done       chan struct{}
	firstFrame chan struct{}
	stop       sync.Once
	mu         sync.Mutex
	err        error
}

func (v *viewerProcess) Done() <-chan struct{}       { return v.done }
func (v *viewerProcess) FirstFrame() <-chan struct{} { return v.firstFrame }
func (v *viewerProcess) Wait() error                 { <-v.done; v.mu.Lock(); defer v.mu.Unlock(); return v.err }
func (v *viewerProcess) Stop() error {
	v.stop.Do(func() { _ = v.stdin.Close() })
	timer := time.NewTimer(3 * time.Second)
	defer timer.Stop()
	select {
	case <-v.done:
	case <-timer.C:
		_ = v.cmd.Process.Kill()
		<-v.done
	}
	return v.Wait()
}
func startViewer(ctx context.Context, binaryPath string, d descriptor) (viewer, error) {
	// Rehash immediately before exec, after source startup. Neither viewer path
	// nor profile may be substituted through the descriptor.
	if err := localcomponents.VerifyPrepared(binaryPath); err != nil {
		return nil, err
	}
	cmd := exec.CommandContext(ctx, binaryPath, "--source-preview-stdin")
	cmd.Env = previewEnvironment(os.Environ())
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		stdin.Close()
		return nil, err
	}
	if err := cmd.Start(); err != nil {
		stdin.Close()
		return nil, err
	}
	v := &viewerProcess{cmd: cmd, stdin: stdin, done: make(chan struct{}), firstFrame: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(stdout)
		scanner.Buffer(make([]byte, 4096), 64<<10)
		seenReady, seenFrame, seenStop := false, false, false
		var protocolErr error
		for scanner.Scan() {
			var event viewerEvent
			if componentjson.Decode(scanner.Bytes(), &event) != nil || event.SchemaVersion != 1 || event.SessionID != d.SessionID || seenStop {
				protocolErr = errors.New("invalid viewer event")
				break
			}
			switch event.Event {
			case "ready":
				if seenReady || event.Reason != "" {
					protocolErr = errors.New("invalid viewer readiness")
				} else {
					seenReady = true
					ready <- nil
				}
			case "first_frame":
				if !seenReady || seenFrame || event.Reason != "" {
					protocolErr = errors.New("invalid viewer frame event")
				} else {
					seenFrame = true
					close(v.firstFrame)
				}
			case "stopped":
				if !seenReady || (event.Reason != "completed" && event.Reason != "failed") {
					protocolErr = errors.New("invalid viewer terminal event")
				} else {
					seenStop = true
					if event.Reason != "completed" {
						protocolErr = errors.New("viewer failed")
					}
				}
			default:
				protocolErr = errors.New("unsupported viewer event")
			}
			if protocolErr != nil {
				break
			}
		}
		if scanner.Err() != nil || !seenReady || !seenStop {
			protocolErr = errors.New("incomplete viewer lifecycle")
		}
		if protocolErr != nil {
			_ = cmd.Process.Kill()
		}
		exitErr := cmd.Wait()
		if protocolErr == nil && exitErr != nil {
			protocolErr = errors.New("viewer exit failed")
		}
		if !seenReady {
			ready <- errors.New("viewer did not become ready")
		}
		v.mu.Lock()
		v.err = protocolErr
		v.mu.Unlock()
		close(v.done)
	}()
	write := make(chan error, 1)
	go func() { write <- json.NewEncoder(stdin).Encode(d) }()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	fail := func() (viewer, error) { _ = v.Stop(); return nil, errors.New("preview viewer startup failed") }
	select {
	case err := <-write:
		if err != nil {
			return fail()
		}
	case <-ctx.Done():
		return fail()
	case <-timer.C:
		return fail()
	}
	select {
	case err := <-ready:
		if err != nil {
			return fail()
		}
		return v, nil
	case <-ctx.Done():
		return fail()
	case <-timer.C:
		return fail()
	}
}

// Source preview has no recording grant or diagnostic decoder override. The
// normal renderer has environment-driven frame dump hooks, so none of its
// USBRIDGE_* settings may be inherited by this purpose-limited child.
func previewEnvironment(env []string) []string {
	filtered := make([]string, 0, len(env))
	for _, entry := range env {
		name, _, _ := strings.Cut(entry, "=")
		if strings.HasPrefix(strings.ToUpper(name), "USBRIDGE_") {
			continue
		}
		filtered = append(filtered, entry)
	}
	return filtered
}
