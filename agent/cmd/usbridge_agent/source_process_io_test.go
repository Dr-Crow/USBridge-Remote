package main

import (
	"bytes"
	"context"
	"sync"
	"testing"
)

type blockedSourceOutput struct {
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func (w *blockedSourceOutput) Write(p []byte) (int, error) {
	w.mu.Lock()
	w.calls++
	w.mu.Unlock()
	<-w.release
	return len(p), nil
}
func TestSourceOutputCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	out := &blockedSourceOutput{release: make(chan struct{})}
	defer close(out.release)
	writer := &sourceProcessOutput{context: ctx, output: out}
	cancel()
	if _, err := writer.Write([]byte("ready")); err == nil {
		t.Fatal("canceled output accepted")
	}
	if _, err := writer.Write([]byte("another")); err == nil {
		t.Fatal("failed pipe reused")
	}
}
func TestSourceOutputCopies(t *testing.T) {
	var out bytes.Buffer
	writer := &sourceProcessOutput{context: context.Background(), output: &out}
	if n, err := writer.Write([]byte("ready\n")); err != nil || n != 6 || out.String() != "ready\n" {
		t.Fatal("failed protocol output", n, err)
	}
}
