//go:build windows

package main

import (
	"context"
	"errors"
	"testing"
	"time"
)

func signatureBudgetTestChild() *child {
	p := &child{
		owner: &job{}, done: make(chan struct{}),
		packets: make(chan protocolPacket, 2), drained: make(chan error, 1),
	}
	close(p.done)
	return p
}

func TestWindowsSignatureBudgetRejectsReadyExpiredStages(t *testing.T) {
	ctx := signatureDelayedDeadline{context.Background()}
	p := signatureBudgetTestChild()
	p.packets <- protocolPacket{line: []byte("ready\n")}
	p.drained <- nil
	if err := p.signatureWaitContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("natural wait beat absolute deadline")
	}
	if _, err := p.signatureNextContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("result beat absolute deadline")
	}
	if err := p.signatureFinishProtocolContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("protocol EOF beat absolute deadline")
	}
}

func TestWindowsSignatureBudgetCoversEOFAndDrain(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	p := signatureBudgetTestChild()
	close(p.packets)
	if err := p.signatureFinishProtocolContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("stderr drain did not share caller deadline")
	}
	p.drained <- nil
	if err := p.signatureFinishProtocolContext(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("finished drain renewed elapsed deadline")
	}
}

func TestWindowsSignatureBudgetPreservesProtocolFailures(t *testing.T) {
	p := signatureBudgetTestChild()
	p.packets <- protocolPacket{line: []byte("extra\n")}
	if err := p.signatureFinishProtocolContext(context.Background()); err == nil || err.Error() != "extra_child_output" {
		t.Fatal("extra output accepted")
	}
	p = signatureBudgetTestChild()
	close(p.packets)
	p.drained <- failure("unexpected_child_stderr")
	if err := p.signatureFinishProtocolContext(context.Background()); err == nil || err.Error() != "unexpected_child_stderr" {
		t.Fatal("stderr failure lost")
	}
	p = signatureBudgetTestChild()
	close(p.packets)
	p.drained <- nil
	if err := p.signatureWaitContext(context.Background()); err != nil {
		t.Fatal(err)
	}
	if err := p.signatureFinishProtocolContext(context.Background()); err != nil {
		t.Fatal(err)
	}
}
