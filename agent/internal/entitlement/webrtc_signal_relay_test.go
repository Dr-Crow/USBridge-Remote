package entitlement

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"

	"github.com/gorilla/websocket"
)

func TestSignalRelayURL_SwapsSchemeAndCarriesHwID(t *testing.T) {
	prev := TestSetBackendBaseURL("https://usbridge-entitlement.example.test")
	defer TestSetBackendBaseURL(prev)

	got, err := signalRelayURL("hw-123")
	if err != nil {
		t.Fatalf("signalRelayURL: %v", err)
	}
	u, err := url.Parse(got)
	if err != nil {
		t.Fatalf("parse result: %v", err)
	}
	if u.Scheme != "wss" {
		t.Errorf("scheme = %q, want wss", u.Scheme)
	}
	if u.Host != "usbridge-entitlement.example.test" {
		t.Errorf("host = %q, want usbridge-entitlement.example.test", u.Host)
	}
	if u.Path != "/v1/webrtc/signal-relay/connect" {
		t.Errorf("path = %q, want /v1/webrtc/signal-relay/connect", u.Path)
	}
	if got := u.Query().Get("hw_id"); got != "hw-123" {
		t.Errorf("hw_id query param = %q, want hw-123", got)
	}
}

func TestSignalRelayURL_HTTPBecomesWS(t *testing.T) {
	// httptest.Server URLs are plain http:// -- confirms local test servers
	// (as DialSignalRelay's own tests below use) resolve to a dialable ws://
	// rather than wss://, which would fail TLS against a plain httptest.Server.
	prev := TestSetBackendBaseURL("http://127.0.0.1:9999")
	defer TestSetBackendBaseURL(prev)

	got, err := signalRelayURL("hw-123")
	if err != nil {
		t.Fatalf("signalRelayURL: %v", err)
	}
	if !strings.HasPrefix(got, "ws://127.0.0.1:9999/v1/webrtc/signal-relay/connect") {
		t.Errorf("got %q, want a ws:// URL", got)
	}
}

func TestDialSignalRelay_Success(t *testing.T) {
	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Query().Get("hw_id") != "hw-123" {
			t.Errorf("unexpected hw_id query: %s", r.URL.RawQuery)
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		// Echo one message so the client side has something to read,
		// proving the connection is genuinely usable, not just accepted.
		_, msg, err := conn.ReadMessage()
		if err != nil {
			return
		}
		_ = conn.WriteMessage(websocket.TextMessage, msg)
	}))
	defer srv.Close()
	prev := TestSetBackendBaseURL(srv.URL)
	defer TestSetBackendBaseURL(prev)

	conn, err := DialSignalRelay(context.Background(), "hw-123")
	if err != nil {
		t.Fatalf("DialSignalRelay: %v", err)
	}
	defer conn.Close()

	if err := conn.WriteMessage(websocket.TextMessage, []byte("ping")); err != nil {
		t.Fatalf("write: %v", err)
	}
	_, got, err := conn.ReadMessage()
	if err != nil {
		t.Fatalf("read: %v", err)
	}
	if string(got) != "ping" {
		t.Errorf("echoed message = %q, want ping", got)
	}
}

func TestDialSignalRelay_NotPro(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusForbidden)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "not_pro"})
	}))
	defer srv.Close()
	prev := TestSetBackendBaseURL(srv.URL)
	defer TestSetBackendBaseURL(prev)

	_, err := DialSignalRelay(context.Background(), "hw-free")
	refused, ok := err.(*SignalRelayRefused)
	if !ok {
		t.Fatalf("err = %v (%T), want *SignalRelayRefused", err, err)
	}
	if refused.Reason != "not_pro" {
		t.Errorf("refused.Reason = %q, want not_pro", refused.Reason)
	}
}

func TestDialSignalRelay_RateLimited(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusTooManyRequests)
		_ = json.NewEncoder(w).Encode(map[string]string{"error": "rate_limited"})
	}))
	defer srv.Close()
	prev := TestSetBackendBaseURL(srv.URL)
	defer TestSetBackendBaseURL(prev)

	_, err := DialSignalRelay(context.Background(), "hw-123")
	refused, ok := err.(*SignalRelayRefused)
	if !ok {
		t.Fatalf("err = %v (%T), want *SignalRelayRefused", err, err)
	}
	if refused.Reason != "rate_limited" {
		t.Errorf("refused.Reason = %q, want rate_limited", refused.Reason)
	}
}
