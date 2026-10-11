// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import (
	"context"
	"time"
)

func joinWorkers(deadline time.Time, workers ...<-chan struct{}) bool {
	timer := time.NewTimer(max(0, time.Until(deadline)))
	defer timer.Stop()
	for _, worker := range workers {
		// Already joined workers remain joined even when the deadline has elapsed.
		select {
		case <-worker:
			continue
		default:
		}
		select {
		case <-worker:
		case <-timer.C:
			return false
		}
	}
	return true
}

// The retirement tolerance is shorter than (and never extends) the run context.
func waitRetiredInventoryContext(ctx context.Context, query func() ([]uint32, error), retired map[uint32]bool, timeout time.Duration) error {
	deadline := time.Now().Add(max(0, timeout))
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		ids, err := query()
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
		select {
		case <-timer.C:
		case <-ctx.Done():
			timer.Stop()
			return ctx.Err()
		}
	}
}
