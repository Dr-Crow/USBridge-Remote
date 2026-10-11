// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestRetirementDoesNotRenewAbsoluteDeadline(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 25*time.Millisecond)
	defer cancel()
	started := time.Now()
	queries := 0
	err := waitRetiredInventoryContext(ctx, func() ([]uint32, error) { queries++; return nil, errIncompleteInventory }, map[uint32]bool{1: true}, time.Hour)
	if !errors.Is(err, context.DeadlineExceeded) || queries < 1 || time.Since(started) > time.Second {
		t.Fatal("retirement renewed or ignored caller deadline")
	}
	// The same context stays expired, even if a new stage would immediately succeed.
	queries = 0
	err = waitRetiredInventoryContext(ctx, func() ([]uint32, error) { queries++; return nil, nil }, nil, time.Hour)
	if !errors.Is(err, context.DeadlineExceeded) || queries != 0 {
		t.Fatal("expired run restarted")
	}
}
func TestRetirementCancelAndNegativeInventory(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := waitRetiredInventoryContext(ctx, func() ([]uint32, error) { t.Fatal("query after cancel"); return nil, nil }, nil, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	for _, ids := range [][]uint32{{0}, {9}, {1, 1}} {
		if waitRetiredInventoryContext(context.Background(), func() ([]uint32, error) { return ids, errIncompleteInventory }, map[uint32]bool{1: true}, time.Hour) == nil {
			t.Fatal("unknown inventory accepted")
		}
	}
}
func TestJoinRequiresEveryWorkerAndOneBudget(t *testing.T) {
	done, blocked := make(chan struct{}), make(chan struct{})
	close(done)
	if !joinWorkers(time.Now().Add(-time.Second), done, done) {
		t.Fatal("already joined rejected")
	}
	started := time.Now()
	if joinWorkers(time.Now().Add(20*time.Millisecond), done, blocked, blocked) {
		t.Fatal("unfinished worker accepted")
	}
	if time.Since(started) > time.Second {
		t.Fatal("join deadline renewed")
	}
}
