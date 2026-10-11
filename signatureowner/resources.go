// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import (
	"errors"
	"sync"
)

// ResourceCloseError means an owned handle/file close could not be verified.
// Resources contains bounded fixed categories, never paths or raw OS messages.
// errors.As finds this type; errors.Is still finds a preserved primary error.
type ResourceCloseError struct{ Resources []string }

func (*ResourceCloseError) Error() string { return "signature_resource_close_uncertain" }

type resourceTracker struct {
	mu     sync.Mutex
	failed map[string]bool
}

func (r *resourceTracker) record(resource string, err error) {
	if err == nil {
		return
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failed == nil {
		r.failed = map[string]bool{}
	}
	r.failed[resource] = true
}
func (r *resourceTracker) failures() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	var out []string
	// Fixed order also bounds this diagnostic regardless of dependency count.
	for _, name := range []string{"work", "script", "power", "dependency", "console_file", "root_observed_handle", "console_handle", "stdin", "stdout", "stderr", "root_handle", "wait_handle", "job_handle", "launch_pipe", "launch_thread", "launch_process", "inspection_handle"} {
		if r.failed[name] {
			out = append(out, name)
		}
	}
	return out
}
func (r *resourceTracker) finish(result *Result, primary *error) {
	failures := r.failures()
	var already *ResourceCloseError
	if errors.As(*primary, &already) {
		failures = append(failures, already.Resources...)
	}
	result.ResourcesReleased = len(failures) == 0 && (!result.WatchdogJoined || result.CleanupJoined)
	if len(failures) > 0 {
		result.ResourceCloseFailures = failures
		*primary = errors.Join(*primary, &ResourceCloseError{Resources: failures})
		clear(result.Output)
		result.Output = nil
	}
}
func closeFailure(primary error, resource string, err error) error {
	if err == nil {
		return primary
	}
	return errors.Join(primary, &ResourceCloseError{Resources: []string{resource}})
}
