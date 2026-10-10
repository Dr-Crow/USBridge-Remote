package sourcebroker

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"sync"
	"time"

	"usbridge_agent/internal/localcomponents"
)

// RunStdio supervises one explicit TLS importer-client session. No listening
// socket, stock broker protocol, or OS USB device is created by the agent.
func RunStdio(ctx context.Context, options localcomponents.Options, in io.Reader, out io.Writer) error {
	reader := bufio.NewReaderSize(in, MaxMessage+1)
	raw, err := reader.ReadSlice('\n')
	if err != nil || len(raw) > MaxMessage {
		return errors.New("source-broker expects one bounded launch JSON line")
	}
	var launch Launch
	if err := decode(raw, &launch); err != nil {
		return err
	}
	if err := launch.Validate(); err != nil {
		return err
	}
	if options.ManifestSHA256 == "" {
		return errors.New("source-broker needs a pinned component manifest SHA-256")
	}
	result, err := localcomponents.Resolve(ctx, options, "source-broker")
	if err != nil {
		return fmt.Errorf("resolve source-broker: %w", err)
	}
	if result.Profile != Profile {
		return errors.New("component does not implement source-broker-v1")
	}
	if err := localcomponents.VerifyPrepared(result.Binary); err != nil {
		return fmt.Errorf("verify source-broker: %w", err)
	}
	if err := checkCapabilities(ctx, result.Binary); err != nil {
		return err
	}
	return runBinary(ctx, result.Binary, launch, reader, out)
}

func runBinary(ctx context.Context, binary string, launch Launch, in io.Reader, out io.Writer) error {
	childCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	cmd := exec.CommandContext(childCtx, binary, "--session-stdin")
	cmd.Stderr = io.Discard
	cmd.WaitDelay = 2 * time.Second
	childIn, err := cmd.StdinPipe()
	if err != nil {
		return err
	}
	defer childIn.Close()
	cmd.Cancel = func() error { _ = childIn.Close(); return nil }
	childOut, err := cmd.StdoutPipe()
	if err != nil {
		return err
	}
	if err := cmd.Start(); err != nil {
		return errors.New("start source-broker process")
	}
	startupDeadline := time.AfterFunc(10*time.Second, cancel)
	defer startupDeadline.Stop()
	events := make(chan Event, 1)
	readErrors := make(chan error, 1)
	processDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(childOut)
		scanner.Buffer(make([]byte, 4096), MaxMessage)
		for scanner.Scan() {
			var event Event
			if err := decode(scanner.Bytes(), &event); err != nil {
				readErrors <- err
				cancel()
				break
			}
			select {
			case events <- event:
			case <-childCtx.Done():
			}
		}
		if scanner.Err() != nil {
			select {
			case readErrors <- errors.New("source-broker output exceeds protocol bounds"):
			default:
			}
			cancel()
		}
		err := cmd.Wait()
		if err != nil {
			processDone <- errors.New("source-broker process failed")
		} else {
			processDone <- nil
		}
	}()
	// Reap on every path, with a bounded graceful EOF before forced shutdown.
	var stopOnce sync.Once
	var exitErr error
	stop := func() {
		stopOnce.Do(func() {
			childIn.Close()
			timer := time.NewTimer(3 * time.Second)
			defer timer.Stop()
			select {
			case exitErr = <-processDone:
			case <-timer.C:
				_ = cmd.Process.Kill()
				cancel()
				exitErr = <-processDone
			}
		})
	}
	defer stop()
	first, _ := json.Marshal(launch)
	first = append(first, '\n')
	if _, err := childIn.Write(first); err != nil {
		return errors.New("send source-broker launch request")
	}
	readyTimer := time.NewTimer(10 * time.Second)
	defer readyTimer.Stop()
	var ready Event
	select {
	case ready = <-events:
		if err := ready.validateReady(launch); err != nil {
			return err
		}
	case err := <-readErrors:
		return err
	case err := <-processDone:
		stopOnce.Do(func() { exitErr = err })
		return errors.New("source-broker exited before readiness")
	case <-readyTimer.C:
		return errors.New("source-broker readiness timed out")
	case <-ctx.Done():
		return ctx.Err()
	}
	startupDeadline.Stop()
	encoder := json.NewEncoder(out)
	if err := encoder.Encode(ready); err != nil {
		return errors.New("write source-broker readiness")
	}
	inputDone := make(chan error, 1)
	go func() {
		scanner := bufio.NewScanner(in)
		scanner.Buffer(make([]byte, 4096), MaxMessage)
		for scanner.Scan() {
			var request Command
			if err := decode(scanner.Bytes(), &request); err != nil {
				inputDone <- err
				return
			}
			if err := request.Validate(); err != nil {
				inputDone <- err
				return
			}
			line := append(append([]byte{}, scanner.Bytes()...), '\n')
			if _, err := childIn.Write(line); err != nil {
				inputDone <- errors.New("source-broker input closed")
				return
			}
			if request.Op == "close" {
				childIn.Close()
				inputDone <- nil
				return
			}
		}
		if scanner.Err() != nil {
			inputDone <- errors.New("source-broker input exceeds protocol bounds")
			return
		}
		childIn.Close()
		inputDone <- nil
	}()
	inputClosed := false
	for {
		select {
		case event := <-events:
			if err := event.validateStatus(); err != nil {
				return err
			}
			if err := encoder.Encode(event); err != nil {
				return errors.New("write source-broker status")
			}
			if event.Event == "error" && event.Error == "session_closed" {
				return errors.New("source-broker remote session closed")
			}
			if event.Event == "stopped" {
				stop()
				return exitErr
			}
		case err := <-readErrors:
			return err
		case err := <-inputDone:
			if err != nil {
				return err
			}
			inputClosed = true
		case err := <-processDone:
			stopOnce.Do(func() { exitErr = err })
			// Wait runs after the reader enqueues its final frame. Preserve that
			// typed terminal event even when the child exits nonzero immediately
			// after reporting an idle remote-session loss.
			select {
			case event := <-events:
				if statusErr := event.validateStatus(); statusErr != nil {
					return statusErr
				}
				if writeErr := encoder.Encode(event); writeErr != nil {
					return errors.New("write source-broker status")
				}
				if event.Event == "error" && event.Error == "session_closed" {
					return errors.New("source-broker remote session closed")
				}
				if event.Event == "stopped" {
					return exitErr
				}
			default:
			}
			if err != nil {
				return err
			}
			if inputClosed {
				return errors.New("source-broker exited without stopped acknowledgement")
			}
			return errors.New("source-broker exited unexpectedly")
		case <-ctx.Done():
			return ctx.Err()
		}
	}
}
