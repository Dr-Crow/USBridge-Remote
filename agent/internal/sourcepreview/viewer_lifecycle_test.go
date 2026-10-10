package sourcepreview

import (
	"errors"
	"testing"

	"usbridge_agent/internal/previewprocess"
)

func TestViewerLifecyclePreservesTypedOwnerFailures(t *testing.T) {
	if err := viewerLifecycleError(nil, nil, nil); err != nil {
		t.Fatal(err)
	}
	for _, ownerErr := range []error{previewprocess.ErrForcedCleanup, previewprocess.ErrCleanupTimeout, previewprocess.ErrContainment, previewprocess.ErrExitStatus} {
		for _, protocolErr := range []error{nil, errViewerProtocol} {
			err := viewerLifecycleError(protocolErr, ownerErr, nil)
			if !errors.Is(err, ownerErr) {
				t.Fatal("owner failure erased")
			}
			if protocolErr != nil && !errors.Is(err, errViewerProtocol) {
				t.Fatal("protocol failure erased")
			}
		}
	}
	if !errors.Is(viewerLifecycleError(nil, nil, previewprocess.ErrCleanupTimeout), previewprocess.ErrCleanupTimeout) {
		t.Fatal("request writer join failure erased")
	}
}
