// SPDX-License-Identifier: GPL-3.0-only
package signatureowner

import (
	"errors"
	"strings"
	"sync"
	"testing"
)

func TestFailedFinalReleaseCannotReturnSuccessfulOutput(t *testing.T) {
	for _, resource := range []string{"work", "script", "power", "dependency", "console_file", "stdin", "stdout", "stderr", "job_handle", "launch_thread", "root_handle", "wait_handle"} {
		t.Run(resource, func(t *testing.T) {
			operation := func() (result Result, err error) {
				tracker := &resourceTracker{}
				defer tracker.finish(&result, &err)
				defer func() { tracker.record(resource, errors.New("PRIVATE PATH AND OS DETAIL")) }()
				result.Output = []byte("{}\n")
				result.NaturalCleanup = true
				result.CleanupJoined = true
				result.WatchdogJoined = true
				return result, nil
			}
			result, err := operation()
			var uncertain *ResourceCloseError
			if err == nil || !errors.As(err, &uncertain) || result.ResourcesReleased || len(result.Output) != 0 || len(result.ResourceCloseFailures) != 1 {
				t.Fatal("close failure returned clean success")
			}
			if strings.Contains(err.Error(), "PRIVATE") {
				t.Fatal("raw close failure disclosed")
			}
		})
	}
}
func TestResourceFailurePreservesPrimaryAndBoundedConcurrentCategories(t *testing.T) {
	primary := errors.New("primary_failure")
	err := primary
	result := Result{}
	tracker := &resourceTracker{}
	var group sync.WaitGroup
	for i := 0; i < 100; i++ {
		group.Add(1)
		go func() { defer group.Done(); tracker.record("dependency", errors.New("private")) }()
	}
	group.Wait()
	tracker.finish(&result, &err)
	var uncertain *ResourceCloseError
	if !errors.Is(err, primary) || !errors.As(err, &uncertain) || len(uncertain.Resources) != 1 {
		t.Fatal("primary lost or unbounded close report")
	}
}
func TestEarlyTypedCloseFailureAndUnjoinedWorkerRemainUncertain(t *testing.T) {
	primary := errors.New("primary")
	err := closeFailure(primary, "script", errors.New("private"))
	result := Result{Output: []byte("{}")}
	(&resourceTracker{}).finish(&result, &err)
	if !errors.Is(err, primary) || result.ResourcesReleased || len(result.Output) != 0 {
		t.Fatal("early failed release hidden")
	}
	result = Result{WatchdogJoined: true, CleanupJoined: false}
	err = errors.New("join_failed")
	(&resourceTracker{}).finish(&result, &err)
	if result.ResourcesReleased {
		t.Fatal("unfinished worker reported fully released")
	}
	result = Result{}
	err = nil
	(&resourceTracker{}).finish(&result, &err)
	if err != nil || !result.ResourcesReleased {
		t.Fatal("successful no-resource release rejected")
	}
}
