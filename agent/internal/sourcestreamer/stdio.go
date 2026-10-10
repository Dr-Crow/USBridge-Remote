package sourcestreamer

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"io"

	"usbridge_agent/internal/localcomponents"
)

// RunStdio is the explicit headless experiment. The parent supplies one launch
// JSON line, keeps stdin open during the session, and closes it to stop. No
// remote listener or stock agent capability is enabled by this entrypoint.
func RunStdio(ctx context.Context, options localcomponents.Options, in io.Reader, out io.Writer) error {
	reader := bufio.NewReaderSize(in, MaxMessage+1)
	frame, err := reader.ReadSlice('\n')
	if err != nil || len(frame) > MaxMessage {
		return errors.New("source-streamer expects one bounded launch JSON line")
	}
	request, err := DecodeLaunch(bytes.NewReader(frame))
	if err != nil {
		return err
	}
	session, err := Start(ctx, options, request)
	if err != nil {
		return err
	}
	defer session.Stop()
	encoder := json.NewEncoder(out)
	if err := encoder.Encode(session.Ready); err != nil {
		return errors.New("write source-streamer readiness")
	}
	inputDone := make(chan error, 1)
	go func() {
		_, err := reader.ReadByte()
		if err == io.EOF {
			inputDone <- nil
		} else {
			inputDone <- errors.New("unexpected source-streamer control input")
		}
	}()
	var runErr error
	select {
	case err := <-inputDone:
		if err != nil {
			runErr = err
		}
		if stopErr := session.Stop(); runErr == nil {
			runErr = stopErr
		}
	case <-session.Done():
		runErr = session.Wait()
	case <-ctx.Done():
		session.Stop()
		runErr = ctx.Err()
	}
	terminal, ok := session.Terminal()
	if !ok {
		return errors.New("source-streamer terminal status missing")
	}
	if err := encoder.Encode(terminal); err != nil {
		return err
	}
	return runErr
}
