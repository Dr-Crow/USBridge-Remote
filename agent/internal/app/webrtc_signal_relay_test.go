package app

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestReplayOfferTo_Success(t *testing.T) {
	var gotHeaders http.Header
	var gotBody map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotHeaders = r.Header.Clone()
		_ = json.NewDecoder(r.Body).Decode(&gotBody)
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"sdp": "v=0 answer"})
	}))
	defer srv.Close()

	raw := []byte(`{"type":"offer","request_id":"req-1","hw_id":"hw-123","sdp":"v=0 offer"}`)
	sdp, err := replayOfferTo(srv.URL, raw, map[string]string{"X-Auth-Timestamp": "123", "X-Auth-Signature": "sig"})
	if err != nil {
		t.Fatalf("replayOfferTo: %v", err)
	}
	if sdp != "v=0 answer" {
		t.Errorf("sdp = %q, want %q", sdp, "v=0 answer")
	}
	if gotHeaders.Get("X-Auth-Timestamp") != "123" || gotHeaders.Get("X-Auth-Signature") != "sig" {
		t.Errorf("auth headers not forwarded: %v", gotHeaders)
	}
	// The relay envelope's extra fields (type/request_id/hw_id) ride along
	// unchanged -- rustshine's OfferRequest just ignores what it doesn't
	// recognize -- confirming this is a byte-for-byte replay, not a
	// reconstructed body.
	if gotBody["request_id"] != "req-1" || gotBody["sdp"] != "v=0 offer" {
		t.Errorf("unexpected forwarded body: %v", gotBody)
	}
}

func TestReplayOfferTo_NonOKStatus(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "bad offer", http.StatusBadRequest)
	}))
	defer srv.Close()

	_, err := replayOfferTo(srv.URL, []byte(`{}`), nil)
	if err == nil {
		t.Fatal("expected an error for a non-200 rustshine response")
	}
}

func TestHandleSignalRelayOffer_WritesAnswerBackOverTheSocket(t *testing.T) {
	rustshine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"sdp": "v=0 answer"})
	}))
	defer rustshine.Close()
	prevURL := rustshineWebRTCOfferURL
	rustshineWebRTCOfferURL = rustshine.URL
	defer func() { rustshineWebRTCOfferURL = prevURL }()

	upgrader := websocket.Upgrader{}
	done := make(chan struct{})
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			t.Errorf("upgrade: %v", err)
			return
		}
		defer conn.Close()
		_, raw, err := conn.ReadMessage()
		if err != nil {
			t.Errorf("server read: %v", err)
			return
		}
		var writeMu sync.Mutex
		handleSignalRelayOffer(conn, &writeMu, raw)
		close(done)
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]
	clientConn, _, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer clientConn.Close()

	offer := []byte(`{"type":"offer","request_id":"req-1","sdp":"v=0 offer"}`)
	if err := clientConn.WriteMessage(websocket.TextMessage, offer); err != nil {
		t.Fatalf("client write: %v", err)
	}

	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("timed out waiting for server to process the offer")
	}

	_, got, err := clientConn.ReadMessage()
	if err != nil {
		t.Fatalf("client read answer: %v", err)
	}
	var answer struct {
		Type      string `json:"type"`
		RequestID string `json:"request_id"`
		SDP       string `json:"sdp"`
	}
	if err := json.Unmarshal(got, &answer); err != nil {
		t.Fatalf("unmarshal answer: %v", err)
	}
	if answer.Type != "answer" || answer.RequestID != "req-1" || answer.SDP != "v=0 answer" {
		t.Errorf("unexpected answer: %+v", answer)
	}
}

// TestHandleSignalRelayOffer_ConcurrentOffersDoNotRace exercises writeMu:
// gorilla/websocket forbids concurrent writers on one *Conn, and
// dialAndServeSignalRelay handles each incoming offer in its own goroutine
// -- this pins that handleSignalRelayOffer's shared writeMu actually
// serializes those writes instead of racing. Calls the real
// handleSignalRelayOffer (not a reimplementation), redirected at a local
// rustshine stand-in via rustshineWebRTCOfferURL. Run with -race in CI.
func TestHandleSignalRelayOffer_ConcurrentOffersDoNotRace(t *testing.T) {
	rustshine := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]string{"sdp": "v=0 answer"})
	}))
	defer rustshine.Close()
	prevURL := rustshineWebRTCOfferURL
	rustshineWebRTCOfferURL = rustshine.URL
	defer func() { rustshineWebRTCOfferURL = prevURL }()

	upgrader := websocket.Upgrader{}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		conn, err := upgrader.Upgrade(w, r, nil)
		if err != nil {
			return
		}
		defer conn.Close()

		var writeMu sync.Mutex
		var wg sync.WaitGroup
		for i := 0; i < 10; i++ {
			_, raw, err := conn.ReadMessage()
			if err != nil {
				break
			}
			msg := raw
			wg.Add(1)
			go func() {
				defer wg.Done()
				handleSignalRelayOffer(conn, &writeMu, msg)
			}()
		}
		wg.Wait()
	}))
	defer srv.Close()

	wsURL := "ws" + srv.URL[len("http"):]
	clientConn, _, err := websocket.DefaultDialer.DialContext(context.Background(), wsURL, nil)
	if err != nil {
		t.Fatalf("client dial: %v", err)
	}
	defer clientConn.Close()

	for i := 0; i < 10; i++ {
		offer := []byte(`{"type":"offer","request_id":"req","sdp":"v=0 offer"}`)
		if err := clientConn.WriteMessage(websocket.TextMessage, offer); err != nil {
			t.Fatalf("client write %d: %v", i, err)
		}
	}
	for i := 0; i < 10; i++ {
		clientConn.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, _, err := clientConn.ReadMessage(); err != nil {
			t.Fatalf("client read %d: %v", i, err)
		}
	}
}
