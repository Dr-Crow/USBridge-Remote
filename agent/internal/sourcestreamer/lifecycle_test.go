package sourcestreamer

import (
	"errors"
	"io"
	"testing"

	"usbridge_agent/internal/previewprocess"
)

func TestLifecyclePreservesTypedOwnerFailures(t *testing.T) {
	terminal := &Stopped{Reason: "completed"}
	if err := lifecycleError(nil, nil, terminal, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, ownerErr := range []error{previewprocess.ErrForcedCleanup, previewprocess.ErrCleanupTimeout, previewprocess.ErrContainment, previewprocess.ErrExitStatus} {
		for _, protocolErr := range []error{nil, ErrProtocol} {
			err := lifecycleError(protocolErr, nil, terminal, ownerErr, nil)
			if !errors.Is(err, ownerErr) {
				t.Fatal("owner failure erased")
			}
			if protocolErr != nil && !errors.Is(err, ErrProtocol) {
				t.Fatal("protocol failure erased")
			}
		}
		failed := &Stopped{Reason: "failed", FailureCode: "frame_too_large"}
		err := lifecycleError(nil, io.ErrClosedPipe, failed, ownerErr, nil)
		if !errors.Is(err, ErrFrameTooLarge) || !errors.Is(err, ownerErr) || !errors.Is(err, ErrProtocol) {
			t.Fatal("independent failure lost")
		}
	}
	if !errors.Is(lifecycleError(nil, nil, terminal, nil, previewprocess.ErrCleanupTimeout), previewprocess.ErrCleanupTimeout) {
		t.Fatal("request writer join failure erased")
	}
}
