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

func TestRuntimeLocalRejectsAuthenticatedCertificateBeforeNetwork(t *testing.T) {
	netpolicy.ConfigureRuntimeLocal(true)
	defer netpolicy.ConfigureRuntimeLocal(false)
	server(t, func(http.ResponseWriter, *http.Request) { t.Error("runtime-local certificate request reached network") })
	if cert, err := RequestCert(context.Background(), "local-hardware-id", []byte("test-csr"), "test-account-token"); cert != nil || !errors.Is(err, netpolicy.ErrRuntimeLocal) {
		t.Fatalf("certificate=%v error=%v", cert, err)
	}
}
