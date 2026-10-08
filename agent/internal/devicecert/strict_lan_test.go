package devicecert

import (
	"context"
	"errors"
	"net/http"
	"testing"
	"usbridge_agent/internal/netpolicy"
)

func TestStrictLANRejectsDNSAndCertificateBeforeNetwork(t *testing.T) {
	t.Setenv(netpolicy.Environment, "1")
	if req, err := newRequest(context.Background(), http.MethodPost, "/v1/device/cert", nil); req != nil || !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("request=%v error=%v", req, err)
	}
	if _, err := RegisterIP(context.Background(), "local-hardware-id", "192.168.1.10"); !errors.Is(err, netpolicy.ErrStrictLAN) {
		t.Fatalf("DNS: %v", err)
	}
}
