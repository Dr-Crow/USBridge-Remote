package main

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"
)

// Advance the absolute deadline synchronously, without depending on how quickly
// a loaded native runner schedules the first query.
type signatureControlledDeadline struct {
	context.Context
	deadline time.Time
}

func (c *signatureControlledDeadline) Deadline() (time.Time, bool) {
	return c.deadline, true
}

func TestSignatureRetirementDoesNotRenewDeadline(t *testing.T) {
	ctx := &signatureControlledDeadline{Context: context.Background(), deadline: time.Now().Add(time.Hour)}
	queries := 0
	err := waitSignatureRetiredInventory(ctx, func() ([]uint32, error) {
		queries++
		ctx.deadline = time.Now().Add(-time.Second)
		return nil, errIncompleteInventory
	}, map[uint32]bool{1: true}, time.Hour)
	if !errors.Is(err, context.DeadlineExceeded) || queries != 1 {
		t.Fatal("retirement renewed or ignored caller deadline")
	}
	queries = 0
	err = waitSignatureRetiredInventory(ctx, func() ([]uint32, error) { queries++; return nil, nil }, nil, time.Hour)
	if !errors.Is(err, context.DeadlineExceeded) || queries != 0 {
		t.Fatal("expired run restarted")
	}
}

func TestSignatureRetirementRejectsLateEmptyResult(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	if err := waitSignatureRetiredInventory(ctx, func() ([]uint32, error) {
		cancel()
		return nil, nil
	}, nil, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal("empty inventory won cancellation race")
	}
}

func TestSignatureRetirementKeepsStrictInventory(t *testing.T) {
	for _, ids := range [][]uint32{{0}, {9}, {1, 1}} {
		if waitSignatureRetiredInventory(context.Background(), func() ([]uint32, error) {
			return ids, errIncompleteInventory
		}, map[uint32]bool{1: true}, time.Hour) == nil {
			t.Fatal("unknown or duplicate inventory accepted")
		}
	}
	wrapped := errors.Join(errIncompleteInventory, failure("substantive_failure"))
	if err := waitSignatureRetiredInventory(context.Background(), func() ([]uint32, error) {
		return nil, wrapped
	}, nil, time.Hour); err != wrapped {
		t.Fatal("wrapped inventory error was retried")
	}
	queries := 0
	if err := waitSignatureRetiredInventory(context.Background(), func() ([]uint32, error) {
		queries++
		if queries == 1 {
			return []uint32{1}, errIncompleteInventory
		}
		return nil, nil
	}, map[uint32]bool{1: true}, time.Second); err != nil || queries != 2 {
		t.Fatal("valid retained-identity retirement failed")
	}
}

// Model a delayed context timer: Deadline is past, while Err and Done have not
// yet reported expiration. A queued success must still lose to the clock.
type signatureDelayedDeadline struct{ context.Context }

func (signatureDelayedDeadline) Deadline() (time.Time, bool) {
	return time.Now().Add(-time.Second), true
}

func TestSignatureReadyResultCannotBeatExpiredClock(t *testing.T) {
	ctx := signatureDelayedDeadline{context.Background()}
	for range 100 {
		ready := make(chan int, 1)
		ready <- 7
		if _, _, err := signatureReceive(ctx, ready); !errors.Is(err, context.DeadlineExceeded) {
			t.Fatal("ready result beat elapsed deadline")
		}
		if len(ready) != 1 {
			t.Fatal("consumed work after deadline")
		}
	}
	done := make(chan struct{})
	close(done)
	if signatureJoinWorkers(ctx, done) {
		t.Fatal("already joined success beat elapsed deadline")
	}
	if err := waitSignatureRetiredInventory(ctx, func() ([]uint32, error) {
		t.Fatal("query after absolute deadline")
		return nil, nil
	}, nil, time.Hour); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal(err)
	}
}

// Deterministically cancel between the receive's precheck and postcheck.
type signatureReceiveRaceContext struct {
	context.Context
	checks int
}

func (c *signatureReceiveRaceContext) Err() error {
	c.checks++
	if c.checks > 1 {
		return context.Canceled
	}
	return nil
}

func TestSignatureReceiveRechecksAfterReadyResult(t *testing.T) {
	ctx := &signatureReceiveRaceContext{Context: context.Background()}
	ready := make(chan int, 1)
	ready <- 7
	if _, _, err := signatureReceive(ctx, ready); !errors.Is(err, context.Canceled) {
		t.Fatal("receive failed to recheck deadline/cancellation")
	}
}

func TestSignatureStagesShareOneAbsoluteBudget(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	deadline, _ := ctx.Deadline()
	// Generous durations keep the ordering test independent of runner load.
	// Each local limit is longer than the remaining parent budget.
	stage, stopStage := context.WithTimeout(ctx, 5*time.Hour)
	defer stopStage()
	stageDeadline, _ := stage.Deadline()
	if !stageDeadline.Equal(deadline) {
		t.Fatal("startup extended parent deadline")
	}
	first := make(chan struct{})
	close(first)
	if _, _, err := signatureReceive(stage, first); err != nil {
		t.Fatal(err)
	}
	input, stopInput := context.WithTimeout(ctx, 3*time.Hour)
	defer stopInput()
	inputDeadline, _ := input.Deadline()
	if !inputDeadline.Equal(deadline) {
		t.Fatal("input extended parent deadline")
	}
	cancel()
	blocked := make(chan struct{})
	if signatureJoinWorkers(input, blocked) {
		t.Fatal("unfinished worker accepted")
	}
	close(blocked)
	if signatureJoinWorkers(ctx, blocked) {
		t.Fatal("later stage renewed parent deadline")
	}
}

func TestSignatureBlockedWaitUsesRemainingDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	blocked := make(chan struct{})
	if _, _, err := signatureReceive(ctx, blocked); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("blocked stage ignored shared deadline")
	}
	close(blocked)
	if signatureJoinWorkers(ctx, blocked) {
		t.Fatal("later ready stage renewed expired deadline")
	}
}

func TestSignatureJoinRequiresEveryWorker(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), time.Hour)
	defer cancel()
	done := make(chan struct{})
	close(done)
	if !signatureJoinWorkers(ctx, done, done) {
		t.Fatal("completed workers rejected")
	}
	canceled, stop := context.WithCancel(context.Background())
	stop()
	if signatureJoinWorkers(canceled, done) {
		t.Fatal("canceled run accepted")
	}
}

func TestSignatureBudgetDiagnosticsAreClosedScalars(t *testing.T) {
	raw, err := json.Marshal(graphicsOSInspection{})
	if err != nil {
		t.Fatal(err)
	}
	var fields map[string]any
	if err = json.Unmarshal(raw, &fields); err != nil {
		t.Fatal(err)
	}
	for _, key := range []string{"verifier_startup_elapsed_ms", "verifier_input_elapsed_ms", "verifier_query_elapsed_ms"} {
		if _, ok := fields[key].(float64); !ok {
			t.Fatalf("missing numeric diagnostic %s", key)
		}
	}
	for _, key := range []string{"verifier_timed_out", "verifier_result_observed_at_timeout", "verifier_root_zero_at_timeout", "verifier_host_zero_at_timeout"} {
		if _, ok := fields[key].(bool); !ok {
			t.Fatalf("missing boolean diagnostic %s", key)
		}
	}
	if signatureOwnerBudget != 12*time.Second {
		t.Fatal("whole-owner budget changed")
	}
}
