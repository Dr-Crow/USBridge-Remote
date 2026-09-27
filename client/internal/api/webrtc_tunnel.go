package api

import (
	"bufio"
	"fmt"
	"io"
	"net"
	"net/http"
)

// webrtcAPITransport implements http.RoundTripper by tunneling HTTP/1.1 requests
// over a WebRTC DataChannel (label "api-tunnel"). This allows the browser web
// client to reach the agent's REST API without triggering mixed-content blocks
// when loaded over HTTPS.
//
// fallback is nil on the wasm build (see usbClientDataChannelFallback in
// usb_client_fallback_wasm.go): a browser session has no legitimate second
// transport to fall back to here -- any direct fetch() this transport could
// attempt instead is exactly the request that shows up permanently
// "(blocked)" in devtools against a relay/WebRTC-only session (mixed
// content, or Chrome's Local Network Access policy blocking a fetch to a
// private-network host from a public-network page), so retrying it every
// poll cycle only spams the console without ever succeeding. Direct/
// Tailscale HTTP stays desktop-native only -- see
// service.MoonlightService.OpenDataChannel's doc comment for why fallback
// is never nil there (its openDataChannel always errors, so every request
// already goes through fallback exactly as before this transport existed).
type webrtcAPITransport struct {
	openDataChannel func(label string) (net.Conn, error)
	fallback        http.RoundTripper
}

func (t *webrtcAPITransport) RoundTrip(req *http.Request) (*http.Response, error) {
	if t.openDataChannel == nil {
		if t.fallback == nil {
			return nil, fmt.Errorf("webrtc api transport: no data channel opener configured")
		}
		return t.fallback.RoundTrip(req)
	}

	conn, err := t.openDataChannel("api-tunnel")
	if err != nil {
		if t.fallback == nil {
			return nil, fmt.Errorf("webrtc api transport: open data channel %q: %w", "api-tunnel", err)
		}
		return t.fallback.RoundTrip(req)
	}

	if err := req.Write(conn); err != nil {
		conn.Close()
		return nil, err
	}

	reader := bufio.NewReader(conn)
	resp, err := http.ReadResponse(reader, req)
	if err != nil {
		conn.Close()
		return nil, err
	}

	// We wrap the response body to ensure the underlying DataChannel is closed
	// when the caller finishes reading the body.
	resp.Body = &connCloser{
		ReadCloser: resp.Body,
		conn:       conn,
	}

	return resp, nil
}

type connCloser struct {
	io.ReadCloser
	conn net.Conn
}

func (c *connCloser) Close() error {
	bodyErr := c.ReadCloser.Close()
	connErr := c.conn.Close()
	if bodyErr != nil {
		return bodyErr
	}
	return connErr
}
