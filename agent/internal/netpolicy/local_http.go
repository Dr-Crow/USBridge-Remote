package netpolicy

import (
	"crypto/tls"
	"crypto/x509"
	"errors"
	"net/http"
	"time"
)

// LocalHTTPClient is reserved for explicitly configured component mirrors.
// It has no environment proxy or hostname resolution path. A supplied CA adds
// trust only to this client, never to the operating system. Redirects are denied
// even to other local addresses: a mirror may not silently select a new origin.
func LocalHTTPClient(caPEM []byte) (*http.Client, error) {
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS12}
	if len(caPEM) != 0 {
		roots := x509.NewCertPool()
		if !roots.AppendCertsFromPEM(caPEM) {
			return nil, errors.New("local mirror CA is not a PEM certificate")
		}
		tlsConfig.RootCAs = roots
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = tlsConfig
	return &http.Client{
		Timeout:   30 * time.Second,
		Transport: localTransport{transport},
		CheckRedirect: func(*http.Request, []*http.Request) error {
			return errors.New("local mirror redirects are disabled")
		},
	}, nil
}

type localTransport struct{ base http.RoundTripper }

func (t localTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if _, err := LocalURL(req.URL.String()); err != nil {
		return nil, err
	}
	return t.base.RoundTrip(req)
}
