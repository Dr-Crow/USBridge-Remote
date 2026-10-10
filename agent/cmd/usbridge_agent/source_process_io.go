package main

import (
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// sourceProcessOutput is deliberately process-scoped. If an inherited stdout
// pipe stops accepting bytes, at most one writer remains blocked while the
// caller tears down the verified child and exits this explicit CLI mode. This
// also works on Windows pipes which do not implement SetWriteDeadline.
type sourceProcessOutput struct {
	context context.Context
	output  io.Writer
	mu      sync.Mutex
	failed  bool
}

func (w *sourceProcessOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	if w.failed {
		return 0, errors.New("source output pipe unavailable")
	}
	data := append([]byte(nil), p...)
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() { n, err := w.output.Write(data); done <- result{n, err} }()
	timer := time.NewTimer(5 * time.Second)
	defer timer.Stop()
	select {
	case r := <-done:
		if r.err != nil {
			w.failed = true
		}
		return r.n, r.err
	case <-timer.C:
		w.failed = true
		return 0, errors.New("source output pipe timed out")
	case <-w.context.Done():
		w.failed = true
		return 0, w.context.Err()
	}
}
