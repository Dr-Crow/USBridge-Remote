package main

import (
	"bytes"
	"context"
	"io"
	"sync"
	"testing"
	"time"
)

type blockedSourceOutput struct {
	release chan struct{}
	mu      sync.Mutex
	calls   int
}

func TestSourceInputCancellationBeforeLaunch(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	in, out := io.Pipe()
	defer in.Close()
	defer out.Close()
	reader := &sourceProcessInput{context: ctx, input: in}
	cancel()
	if _, err := reader.Read(make([]byte, 32)); err == nil {
		t.Fatal("uncanceled input")
	}
	if _, err := reader.Read(make([]byte, 32)); err == nil {
		t.Fatal("failed input reused")
	}
}

func TestSourceInputLaunchDeadline(t *testing.T) {
	in, out := io.Pipe()
	defer in.Close()
	defer out.Close()
	reader := &sourceProcessInput{context: context.Background(), input: in, launchDeadline: time.Now().Add(20 * time.Millisecond)}
	if _, err := reader.Read(make([]byte, 32)); err == nil {
		t.Fatal("initial input did not time out")
	}
}

func TestSourceInputLaunchDisablesInitialDeadline(t *testing.T) {
	reader := &sourceProcessInput{context: context.Background(), input: bytes.NewBufferString("{}\nclose\n")}
	p := make([]byte, 3)
	if n, err := reader.Read(p); n != 3 || err != nil || string(p) != "{}\n" {
		t.Fatal(n, err)
	}
	reader.launchDeadline = time.Now().Add(-time.Second)
	if n, err := reader.Read(p); n != 3 || err != nil || string(p) != "clo" {
		t.Fatal(n, err)
	}
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
