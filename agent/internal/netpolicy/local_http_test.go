package netpolicy

import (
	"encoding/pem"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

type countingTransport struct{ calls atomic.Int32 }

func (t *countingTransport) RoundTrip(*http.Request) (*http.Response, error) {
	t.calls.Add(1)
	return nil, nil
}

func TestLocalTransportRejectsDNSPublicAndPlainLANBeforeTransport(t *testing.T) {
	base := new(countingTransport)
	transport := localTransport{base}
	for _, raw := range []string{"https://example.invalid/a", "https://8.8.8.8/a", "http://192.168.1.1/a"} {
		req, _ := http.NewRequest(http.MethodGet, raw, nil)
		if _, err := transport.RoundTrip(req); err == nil {
			t.Fatalf("accepted %s", raw)
		}
	}
	if base.calls.Load() != 0 {
		t.Fatal("forbidden destination reached the transport")
	}
}

func TestLocalClientTLSAndRedirectBoundaries(t *testing.T) {
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/redirect" {
			http.Redirect(w, r, "https://example.invalid/a", http.StatusFound)
			return
		}
		_, _ = w.Write([]byte("local component"))
	}))
	t.Cleanup(srv.Close)
	ca := pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: srv.Certificate().Raw})
	client, err := LocalHTTPClient(ca)
	if err != nil {
		t.Fatal(err)
	}
	resp, err := client.Get(srv.URL)
	if err != nil {
		t.Fatal(err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatal(resp.StatusCode)
	}
	if resp, err := client.Get(srv.URL + "/redirect"); err == nil {
		_ = resp.Body.Close()
		t.Fatal("redirect permitted")
	}
	withoutCA, _ := LocalHTTPClient(nil)
	if resp, err := withoutCA.Get(srv.URL); err == nil {
		_ = resp.Body.Close()
		t.Fatal("untrusted TLS accepted")
	}
	if _, err := LocalHTTPClient([]byte("not a certificate")); err == nil {
		t.Fatal("invalid CA accepted")
	}
}
