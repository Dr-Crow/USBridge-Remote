//go:build !windows

package previewprocess

import (
	"context"
	"errors"
	"testing"
)

func TestUnsupportedPlatformFailsClosed(t *testing.T) {
	if p, e := Start(context.Background(), validTestSpec()); p != nil || !errors.Is(e, ErrUnsupported) {
		t.Fatal("unsupported launch did not fail closed")
	}
}
