package account

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"usbridge_agent/internal/netpolicy"
)

func TestStrictLANRejectsCachedAccountTokenBeforeNetwork(t *testing.T) {
	t.Setenv(netpolicy.Environment, "1")
	err := doJSON(context.Background(), http.MethodGet, "/manage/api/licenses", nil, "cached-token", nil)
	if !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("account: %v", err)
	}
}
