package previewchild

import (
	"errors"
	"testing"
	"time"

	"usbridge_agent/internal/previewprocess"
)

func TestJoinSharesOneDeadlineAndRetainsTimeout(t *testing.T) {
	closed := make(chan struct{})
	close(closed)
	if err := joinUntil(time.Now().Add(-time.Second), closed, closed); err != nil {
		t.Fatal("already joined work timed out")
	}
	pending := make(chan struct{})
	if !errors.Is(joinUntil(time.Now(), closed, pending), previewprocess.ErrCleanupTimeout) {
		t.Fatal("join uncertainty erased")
	}
}
