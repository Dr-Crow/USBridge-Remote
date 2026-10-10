package main

import (
	"bytes"
	"context"
	"errors"
	"io"
	"sync"
	"time"
)

// sourceProcessInput gives inherited pipes a portable cancellation boundary.
// It is only for these process-scoped CLI modes: process exit owns at most one
// blocked OS read, after all source children have been stopped and joined.
type sourceProcessInput struct {
	context        context.Context
	input          io.Reader
	mu             sync.Mutex
	failed         bool
	launchRead     bool
	launchDeadline time.Time
}

func (r *sourceProcessInput) Read(p []byte) (int, error) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failed {
		return 0, errors.New("source input pipe unavailable")
	}
	if err := r.context.Err(); err != nil {
		r.failed = true
		return 0, err
	}
	if len(p) == 0 {
		return 0, nil
	}
	if r.launchDeadline.IsZero() {
		r.launchDeadline = time.Now().Add(10 * time.Second)
	}
	if !r.launchRead && !time.Now().Before(r.launchDeadline) {
		r.failed = true
		return 0, errors.New("source launch input timed out")
	}
	buffer := make([]byte, min(len(p), 64<<10))
	type result struct {
		n   int
		err error
	}
	done := make(chan result, 1)
	go func() { n, err := r.input.Read(buffer); done <- result{n, err} }()
	var timeout <-chan time.Time
	if !r.launchRead {
		timer := time.NewTimer(time.Until(r.launchDeadline))
		defer timer.Stop()
		timeout = timer.C
	}
	select {
	case value := <-done:
		if value.n < 0 || value.n > len(buffer) {
			r.failed = true
			return 0, errors.New("invalid source input read")
		}
		if bytes.IndexByte(buffer[:value.n], '\n') >= 0 {
			r.launchRead = true
		}
		copy(p, buffer[:value.n])
		return value.n, value.err
	case <-timeout:
		r.failed = true
		return 0, errors.New("source launch input timed out")
	case <-r.context.Done():
		r.failed = true
		return 0, r.context.Err()
	}
}

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
	if err := w.context.Err(); err != nil {
		w.failed = true
		return 0, err
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
