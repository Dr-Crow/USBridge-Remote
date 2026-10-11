// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import (
	"errors"
	"time"
)

// A successful native query can return fewer IDs than its assigned count.
// This is never an empty-Job proof; only retirement may retry the bounded query.
var errIncompleteInventory = errors.New("incomplete_job_inventory")

// waitRetiredInventory is called only after the caller has waited on every
// listed process handle and retained those handles. A signaled process can
// briefly remain in Job accounting. This does not permit a living/unknown child
// or replace the final empty-Job proof with a process exit code.
func waitRetiredInventory(query func() ([]uint32, error), retired map[uint32]bool, timeout time.Duration) error {
	deadline := time.Now().Add(max(0, timeout))
	for {
		ids, err := query()
		// Only the direct native-query sentinel is retryable. A wrapper/join
		// may carry a substantive failure and must fail closed.
		incomplete := err == errIncompleteInventory
		if err != nil && !incomplete {
			return err
		}
		if len(ids) == 0 && !incomplete {
			return nil
		}
		seen := make(map[uint32]bool, len(ids))
		for _, id := range ids {
			if id == 0 || !retired[id] || seen[id] {
				return failure("unretired_job_member")
			}
			seen[id] = true
		}
		if !time.Now().Before(deadline) {
			return failure("owned_job_not_empty")
		}
		time.Sleep(min(10*time.Millisecond, time.Until(deadline)))
	}
}
