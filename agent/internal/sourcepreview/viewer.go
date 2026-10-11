package sourcepreview

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"os"
	"strings"
	"sync"
	"time"

	"usbridge_agent/internal/componentjson"
	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/previewchild"
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

var errViewerProtocol = errors.New("preview viewer protocol failed")

type viewerProcess struct {
	child      *previewchild.Process
	writeDone  chan struct{}
	done       chan struct{}
	firstFrame chan struct{}
	mu         sync.Mutex
	err        error
}

func (v *viewerProcess) Done() <-chan struct{}       { return v.done }
func (v *viewerProcess) FirstFrame() <-chan struct{} { return v.firstFrame }
func (v *viewerProcess) Wait() error                 { <-v.done; v.mu.Lock(); defer v.mu.Unlock(); return v.err }
func (v *viewerProcess) Stop() error {
	err := v.child.Stop(v.done, previewchild.StopGrace)
	select {
	case <-v.done:
		return errors.Join(err, v.Wait())
	default:
		return err
	}
}
func startViewer(ctx context.Context, binaryPath string, d descriptor) (viewer, error) {
	// Rehash immediately before exec, after source startup. Neither viewer path
	// nor profile may be substituted through the descriptor.
	if err := localcomponents.VerifyPrepared(binaryPath); err != nil {
		return nil, err
	}
	processCtx, cancel := context.WithCancel(ctx)
	child, err := previewchild.Start(processCtx, previewchild.Spec{Path: binaryPath,
		Args: []string{"--source-preview-stdin"}, Env: previewEnvironment(os.Environ())})
	if err != nil {
		cancel()
		return nil, err
	}
	v := &viewerProcess{child: child, writeDone: make(chan struct{}), done: make(chan struct{}), firstFrame: make(chan struct{})}
	ready := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(child.Stdout)
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
			child.Abort()
		}
		_ = child.Stdout.Close()
		exitErr := child.Wait()
		writeErr := previewchild.Join(v.writeDone)
		protocolErr = viewerLifecycleError(protocolErr, exitErr, writeErr)
		if !seenReady {
			ready <- errors.New("viewer did not become ready")
		}
		v.mu.Lock()
		v.err = protocolErr
		v.mu.Unlock()
		cancel()
		close(v.done)
	}()
	write := make(chan error, 1)
	go func() {
		defer close(v.writeDone)
		payload, err := json.Marshal(d)
		if err == nil {
			payload = append(payload, '\n')
			_, err = child.Stdin.Write(payload)
		}
		for i := range payload {
			payload[i] = 0
		}
		write <- err
	}()
	timer := time.NewTimer(10 * time.Second)
	defer timer.Stop()
	fail := func() (viewer, error) {
		cancel()
		return nil, errors.Join(errors.New("preview viewer startup failed"), v.Stop())
	}
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

func viewerLifecycleError(protocolErr, exitErr, writeErr error) error {
	if protocolErr != nil {
		protocolErr = errors.Join(errViewerProtocol, protocolErr)
	}
	return errors.Join(protocolErr, exitErr, writeErr)
}
