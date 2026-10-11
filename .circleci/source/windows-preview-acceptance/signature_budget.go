package main

import (
	"context"
	"time"
)

const signatureOwnerBudget = 12 * time.Second

// Check the absolute clock as well as cancellation: a ready result must not win
// against an expired deadline merely because the context timer was delayed.
func signatureBudgetErr(ctx context.Context) error {
	if err := ctx.Err(); err != nil {
		return err
	}
	if deadline, ok := ctx.Deadline(); ok && !time.Now().Before(deadline) {
		return context.DeadlineExceeded
	}
	return nil
}

func signatureReceive[T any](ctx context.Context, ch <-chan T) (value T, open bool, err error) {
	if err = signatureBudgetErr(ctx); err != nil {
		return value, false, err
	}
	select {
	case <-ctx.Done():
		return value, false, ctx.Err()
	case value, open = <-ch:
		return value, open, signatureBudgetErr(ctx)
	}
}

func signatureJoinWorkers(ctx context.Context, workers ...<-chan struct{}) bool {
	for _, worker := range workers {
		if _, _, err := signatureReceive(ctx, worker); err != nil {
			return false
		}
	}
	return signatureBudgetErr(ctx) == nil
}

// The retirement tolerance is shorter than (and never extends) the run context.
func waitSignatureRetiredInventory(ctx context.Context, query func() ([]uint32, error), retired map[uint32]bool, timeout time.Duration) error {
	deadline := time.Now().Add(max(0, timeout))
	for {
		if err := signatureBudgetErr(ctx); err != nil {
			return err
		}
		ids, err := query()
		if budgetErr := signatureBudgetErr(ctx); budgetErr != nil {
			return budgetErr
		}
		// Only the direct native sentinel is retryable, never a wrapped error.
		incomplete := err == errIncompleteInventory
		if err != nil && !incomplete {
			return err
		}
		if len(ids) == 0 && !incomplete {
			return nil
		}
		seen := map[uint32]bool{}
		for _, id := range ids {
			if id == 0 || !retired[id] || seen[id] {
				return failure("unretired_job_member")
			}
			seen[id] = true
		}
		if !time.Now().Before(deadline) {
			return failure("owned_job_not_empty")
		}
		timer := time.NewTimer(min(10*time.Millisecond, time.Until(deadline)))
		_, _, err = signatureReceive(ctx, timer.C)
		timer.Stop()
		if err != nil {
			return err
		}
	}
}
