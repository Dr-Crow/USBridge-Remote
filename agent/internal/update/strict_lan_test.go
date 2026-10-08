package update

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"usbridge_agent/internal/netpolicy"
)

func TestStrictLANRejectsPublicUpdatesBeforeNetwork(t *testing.T) {
	t.Setenv(netpolicy.Environment, "1")
	if got := Check(context.Background(), "0.0.0"); got != nil {
		t.Fatal("strict policy returned an update")
	}
	if _, err := fetchBytes(context.Background(), &http.Client{}, "https://example.invalid/manifest"); !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("metadata: %v", err)
	}
	if _, err := downloadArtifact(context.Background(), &http.Client{}, "https://example.invalid/artifact", "", nil); !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("artifact: %v", err)
	}
}
