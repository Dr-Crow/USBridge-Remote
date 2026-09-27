package gui

import (
	"bufio"
	"context"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"testing"

	"usbridge-client/internal/service"
)

// fakeVideoClient implements service.VideoClient by embedding a nil
// interface value and overriding only OpenDataChannel -- every other method
// promoted from the embedded interface would panic on a nil receiver if
// called, but attachUSBClient only ever takes OpenDataChannel as a bound
// method value, it doesn't call anything else on mw.videoClient here.
type fakeVideoClient struct {
	service.VideoClient
	openDataChannel func(label string) (net.Conn, error)
}

func (f *fakeVideoClient) OpenDataChannel(label string) (net.Conn, error) {
	return f.openDataChannel(label)
}

// serveOneTunnelRequest plays the agent's side of a single HTTP request/
// response pair over conn, mimicking rustshine's usbPassBridgeAPI relay
// (agent/internal/api/usb_passthrough_browser.go) well enough for
// http.ReadRequest/req.Write round-tripping.
func serveOneTunnelRequest(t *testing.T, conn net.Conn) {
	t.Helper()
	defer conn.Close()
	req, err := http.ReadRequest(bufio.NewReader(conn))
	if err != nil {
		return
	}
	_ = req.Body.Close()
	_, _ = fmt.Fprint(conn, "HTTP/1.1 200 OK\r\nContent-Type: application/json\r\nContent-Length: 2\r\n\r\n{}")
}

// TestAttachUSBClientWiresOpenDataChannel pins the fix for the bug reported
// live: in a browser (wasm) WebRTC session, every /api/* poll (drives/local,
// device/info, iso/space, device/status, pcpanel/leds, ...) showed up
// permanently "Blocked" in devtools. Root cause: api.USBClient.SetOpenData
// Channel existed and was fully implemented (client/internal/api/
// webrtc_tunnel.go) but attachUSBClient -- "the common chokepoint every
// connection path (direct, tailscale, ...) routes through" per its own
// startClipboardSync comment -- only ever wired it into mw.clipboardSync,
// never into mw.usbClient itself. So c.openDataChannel stayed nil forever
// and every request fell back to a direct fetch(), which an https-loaded
// page mixed-content-blocks against the agent's plain-http address.
//
// This test stands in for a real WebRTC PeerConnection with a fake
// videoClient whose OpenDataChannel hands back a net.Pipe conn, and asserts
// that once attachUSBClient runs, an API call rides that tunnel instead of
// ever reaching the real network transport -- the httptest server below
// fails the test outright if hit, exactly like a mixed-content block would
// have silently dropped the request in the browser.
func TestAttachUSBClientWiresOpenDataChannel(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Errorf("request reached the network transport instead of the WebRTC data channel tunnel")
	}))
	defer server.Close()

	client := newTestUSBClient(t, server)

	var gotLabel string
	mw := &MainWindow{
		videoClient: &fakeVideoClient{
			openDataChannel: func(label string) (net.Conn, error) {
				gotLabel = label
				serverSide, clientSide := net.Pipe()
				go serveOneTunnelRequest(t, serverSide)
				return clientSide, nil
			},
		},
	}

	got := mw.attachUSBClient(client)
	if got == nil {
		t.Fatal("attachUSBClient returned nil for a non-nil client")
	}

	if err := got.TestConnectionWithContext(context.Background()); err != nil {
		t.Fatalf("expected the tunneled request to succeed, got %v", err)
	}
	if gotLabel != "api-tunnel" {
		t.Errorf("expected the %q DataChannel label, got %q", "api-tunnel", gotLabel)
	}
}

// TestAttachUSBClientFallsBackWithoutWebRTC covers desktop-native builds
// (and a browser session before video/control has connected): OpenData
// Channel errors, so the wiring in attachUSBClient must not break
// Direct/Tailscale -- the request has to fall back to the real network
// transport exactly as it did before SetOpenDataChannel was ever wired up.
func TestAttachUSBClientFallsBackWithoutWebRTC(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	defer server.Close()

	client := newTestUSBClient(t, server)

	mw := &MainWindow{
		videoClient: &fakeVideoClient{
			openDataChannel: func(label string) (net.Conn, error) {
				return nil, fmt.Errorf("webrtc video: not connected")
			},
		},
	}

	got := mw.attachUSBClient(client)
	if got == nil {
		t.Fatal("attachUSBClient returned nil for a non-nil client")
	}

	if err := got.TestConnectionWithContext(context.Background()); err != nil {
		t.Fatalf("expected fallback to the real network transport (Direct/Tailscale) to succeed, got %v", err)
	}
}
