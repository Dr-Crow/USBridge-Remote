package api

import (
	"bytes"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"testing"
)

// newTestUSBClient points a *USBClient at an httptest.Server -- NewUSBClient
// only accepts a separate host/port (it builds baseURL itself), so this
// pulls both back out of the server's URL. Mirrors the gui package's own
// helper of the same name (main_window_connection_test.go).
func newTestUSBClient(t *testing.T, server *httptest.Server) *USBClient {
	t.Helper()
	u, err := url.Parse(server.URL)
	if err != nil {
		t.Fatalf("parse test server URL: %v", err)
	}
	host, portStr, err := net.SplitHostPort(u.Host)
	if err != nil {
		t.Fatalf("split host/port: %v", err)
	}
	port, err := strconv.Atoi(portStr)
	if err != nil {
		t.Fatalf("parse port: %v", err)
	}
	return NewUSBClient(host, port, 5)
}

// canningResponse builds a valid HTTP/1.1 response with a correct
// Content-Length for body, so http.ReadResponse (used by
// webrtcAPITransport.RoundTrip) parses it without hanging on a mismatched
// length.
func cannedResponse(status int, statusText, body string) string {
	return fmt.Sprintf("HTTP/1.1 %d %s\r\nContent-Type: application/json\r\nContent-Length: %d\r\n\r\n%s",
		status, statusText, len(body), body)
}

// TestUSBClientSetOpenDataChannelPrefersTunnel pins the fix for the bug where
// USBClient.SetOpenDataChannel was fully implemented (this file's sibling,
// webrtc_tunnel.go) but nothing in the gui package ever called it on
// mw.usbClient -- only ClipboardSync got wired (see main_window.go's
// attachUSBClient). The symptom: every /api/* poll (drives/local,
// device/info, iso/space, device/status, pcpanel/leds, ...) permanently hit
// mixed-content blocking on any https-loaded browser session, because the
// client always fell back to a direct fetch() instead of ever trying the
// WebRTC "api-tunnel" DataChannel.
//
// This test exercises USBClient.SetOpenDataChannel directly: once wired, a
// request must ride the tunnel instead of touching the network transport at
// all when a channel can be opened.
func TestUSBClientSetOpenDataChannelPrefersTunnel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request reached the network transport instead of the data channel tunnel")
	}))
	defer server.Close()

	client := newTestUSBClient(t, server)

	reqBuf := new(bytes.Buffer)
	conn := &mockConn{
		reader: bytes.NewBufferString(cannedResponse(200, "OK", `{"status":"ok"}`)),
		writer: reqBuf,
	}

	var gotLabel string
	client.SetOpenDataChannel(func(label string) (net.Conn, error) {
		gotLabel = label
		return conn, nil
	})

	resp, err := client.makeRequest("GET", "/api/status", nil)
	if err != nil {
		t.Fatalf("expected tunneled request to succeed, got %v", err)
	}
	if string(resp) != `{"status":"ok"}` {
		t.Errorf("unexpected body: %s", resp)
	}
	if gotLabel != "api-tunnel" {
		t.Errorf("expected the %q DataChannel label, got %q", "api-tunnel", gotLabel)
	}
	if !bytes.Contains(reqBuf.Bytes(), []byte("GET /api/status HTTP/1.1")) {
		t.Errorf("request was not written to the tunnel connection, buffer: %s", reqBuf.String())
	}
	if !conn.closed {
		t.Errorf("expected the tunnel connection to be closed after the response body was consumed")
	}
}

// TestUSBClientSetOpenDataChannelFallsBackWhenChannelUnavailable covers the
// other half of the same wiring: on every desktop-native build (and in the
// browser before a WebRTC PeerConnection/video session exists),
// OpenDataChannel always errors -- see service.MoonlightService's own
// OpenDataChannel doc comment ("DataChannel not supported"). Wiring
// SetOpenDataChannel must not break Direct/Tailscale connections in that
// case: a request must fall back to the client's original network
// transport exactly as it did before this was ever wired up.
func TestUSBClientSetOpenDataChannelFallsBackWhenChannelUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"status":"ok"}`))
	}))
	defer server.Close()

	client := newTestUSBClient(t, server)
	client.SetOpenDataChannel(func(label string) (net.Conn, error) {
		return nil, fmt.Errorf("no peer connection yet")
	})

	resp, err := client.makeRequest("GET", "/api/status", nil)
	if err != nil {
		t.Fatalf("expected fallback to the real network transport to succeed, got %v", err)
	}
	if string(resp) != `{"status":"ok"}` {
		t.Errorf("unexpected body: %s", resp)
	}
}
