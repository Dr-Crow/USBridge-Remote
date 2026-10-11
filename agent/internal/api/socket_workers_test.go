package api

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"usbridge_agent/internal/clipboard"
	"usbridge_agent/internal/usbpass"
)

func awaitSocketResult(t *testing.T, ch <-chan error) error {
	t.Helper()
	select {
	case err := <-ch:
		return err
	case <-time.After(2 * time.Second):
		t.Fatal("socket workers did not join")
		return nil
	}
}

func TestSocketWorkersCloseBeforeJoin(t *testing.T) {
	for _, cancelFirst := range []bool{false, true} {
		t.Run(fmt.Sprint(cancelFirst), func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			local, remote := net.Pipe()
			defer remote.Close()
			var closed atomic.Int32
			w := newSocketWorkers(ctx, func() error { closed.Add(1); return local.Close() })
			entered, left := make(chan struct{}), make(chan struct{})
			w.goWorker(func() { defer close(left); close(entered); _, _ = local.Write([]byte("blocked")) })
			<-entered
			if cancelFirst {
				cancel()
			}
			joined := make(chan error, 1)
			go func() { joined <- w.closeAndJoin() }()
			if err := awaitSocketResult(t, joined); err != nil {
				t.Fatal(err)
			}
			select {
			case <-left:
			default:
				t.Fatal("writer remained live")
			}
			select {
			case <-w.watcherDone:
			default:
				t.Fatal("cancellation watcher remained live")
			}
			if err := w.closeAndJoin(); err != nil || closed.Load() != 1 {
				t.Fatalf("repeat close: %v count=%d", err, closed.Load())
			}
		})
	}
}

func TestSocketWorkersCloseFailureRetained(t *testing.T) {
	want := errors.New("synthetic close failure")
	w := newSocketWorkers(context.Background(), func() error { return errors.Join(net.ErrClosed, want) })
	if err := w.closeAndJoin(); !errors.Is(err, want) {
		t.Fatalf("close failure lost: %v", err)
	}
	if cleanSocketClosed(fmt.Errorf("closed: %w", net.ErrClosed)) != nil {
		t.Fatal("single wrapped closed error rejected")
	}
}

func TestSocketWorkersCanceledAdmissionAndConcurrentJoin(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var closes atomic.Int32
	w := newSocketWorkers(ctx, func() error { closes.Add(1); return nil })
	if closes.Load() != 1 {
		t.Fatal("canceled handoff was not closed synchronously")
	}
	var joined sync.WaitGroup
	for i := 0; i < 20; i++ {
		joined.Add(1)
		go func() {
			defer joined.Done()
			if err := w.closeAndJoin(); err != nil {
				t.Error(err)
			}
		}()
	}
	joined.Wait()
	if closes.Load() != 1 {
		t.Fatal("concurrent retirement closed twice")
	}
}

func TestRelayWebsocketBrokerBothDirectionsAndRetirement(t *testing.T) {
	for _, ending := range []string{"websocket", "broker", "context"} {
		t.Run(ending, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			broker, peer := net.Pipe()
			defer peer.Close()
			done := make(chan error, 1)
			ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				c, err := (&websocket.Upgrader{}).Upgrade(rw, r, nil)
				if err != nil {
					done <- err
					return
				}
				done <- relayWebsocketBroker(ctx, c, broker)
			}))
			defer ts.Close()
			client, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer client.Close()
			_ = peer.SetDeadline(time.Now().Add(time.Second))
			_ = client.SetReadDeadline(time.Now().Add(time.Second))
			if err := client.WriteMessage(websocket.BinaryMessage, []byte("request")); err != nil {
				t.Fatal(err)
			}
			buf := make([]byte, 7)
			if _, err := io.ReadFull(peer, buf); err != nil || string(buf) != "request" {
				t.Fatalf("request %q %v", buf, err)
			}
			write := make(chan error, 1)
			go func() { _, err := peer.Write([]byte("reply")); write <- err }()
			_, data, err := client.ReadMessage()
			if err != nil || string(data) != "reply" {
				t.Fatalf("reply %q %v", data, err)
			}
			if err := awaitSocketResult(t, write); err != nil {
				t.Fatal(err)
			}
			switch ending {
			case "websocket":
				_ = client.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
				client.Close()
			case "broker":
				peer.Close()
			case "context":
				cancel()
			}
			if err := awaitSocketResult(t, done); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestRelayWebsocketBrokerCanceledHandoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	broker, peer := net.Pipe()
	defer peer.Close()
	done := make(chan error, 1)
	ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
		c, err := (&websocket.Upgrader{}).Upgrade(rw, r, nil)
		if err != nil {
			done <- err
			return
		}
		done <- relayWebsocketBroker(ctx, c, broker)
	}))
	defer ts.Close()
	c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	if err := awaitSocketResult(t, done); err != nil {
		t.Fatal(err)
	}
	if _, err := peer.Write([]byte("closed")); err == nil {
		t.Fatal("canceled handoff kept broker open")
	}
}

type failingBrokerWrite struct {
	net.Conn
	err error
}

func (c failingBrokerWrite) Write(data []byte) (int, error) {
	if c.err != nil {
		return 0, c.err
	}
	return len(data) - 1, nil
}

func TestRelayWebsocketBrokerRetainsWriteFailure(t *testing.T) {
	for _, kind := range []string{"short", "substantive"} {
		t.Run(kind, func(t *testing.T) {
			broker, peer := net.Pipe()
			defer peer.Close()
			want := io.ErrShortWrite
			wrapped := failingBrokerWrite{Conn: broker}
			if kind == "substantive" {
				want = errors.New("synthetic relay failure")
				wrapped.err = errors.Join(net.ErrClosed, want)
			}
			done := make(chan error, 1)
			ts := httptest.NewServer(http.HandlerFunc(func(rw http.ResponseWriter, r *http.Request) {
				c, err := (&websocket.Upgrader{}).Upgrade(rw, r, nil)
				if err != nil {
					done <- err
					return
				}
				done <- relayWebsocketBroker(r.Context(), c, wrapped)
			}))
			defer ts.Close()
			c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			if err := c.WriteMessage(websocket.BinaryMessage, []byte("partial")); err != nil {
				t.Fatal(err)
			}
			if err := awaitSocketResult(t, done); !errors.Is(err, want) {
				t.Fatalf("lost transfer error: %v", err)
			}
		})
	}
}

func TestMouseAndClipboardRoutesJoinOnContextCancellation(t *testing.T) {
	for _, route := range []string{"mouse", "clipboard"} {
		t.Run(route, func(t *testing.T) {
			ctx, cancel := context.WithCancel(context.Background())
			defer cancel()
			s := &Server{app: &stubApp{clip: clipboard.NewManager(&fakeBackend{}, 1024)}}
			done := make(chan error, 1)
			ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				r = r.WithContext(ctx)
				if route == "mouse" {
					s.mouseWS(w, r)
				} else {
					s.clipboardWS(w, r)
				}
				done <- nil
			}))
			defer ts.Close()
			c, _, err := websocket.DefaultDialer.Dial("ws"+strings.TrimPrefix(ts.URL, "http"), nil)
			if err != nil {
				t.Fatal(err)
			}
			defer c.Close()
			cancel()
			if err := awaitSocketResult(t, done); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestLegacyAttachRetainsAuthenticationAndUnavailableResponses(t *testing.T) {
	key := []byte("local synthetic key")
	for _, kind := range []string{"unauthorized", "unavailable", "upgrade_failed"} {
		t.Run(kind, func(t *testing.T) {
			s := &Server{sec: NewSecurityMiddleware(key)}
			path := "/api/usb/passthrough/browser-attach"
			if kind != "unauthorized" {
				stamp := strconv.FormatInt(time.Now().Unix(), 10)
				path += "?" + url.Values{"ts": {stamp}, "sig": {CalculateHMAC("GET", path, stamp, "", key)}}.Encode()
			}
			if kind == "upgrade_failed" {
				s.usb = usbpass.New("", t.TempDir(), "fixture", 1, "")
			}
			w := httptest.NewRecorder()
			s.usbPassthroughBrowserAttach(w, httptest.NewRequest("GET", path, nil))
			want := map[string]int{"unauthorized": 401, "unavailable": 501, "upgrade_failed": 400}[kind]
			if w.Code != want {
				t.Fatalf("got %d want %d", w.Code, want)
			}
		})
	}
}

func TestMouseOldOwnerCannotReleaseNewConnection(t *testing.T) {
	s := &Server{app: &stubApp{}}
	one := uint8(1)
	old, next := nextMouseOwner(), nextMouseOwner()
	s.noteMouse(old, MouseRequest{Action: "absolute", ButtonState: &one})
	s.noteMouse(next, MouseRequest{Action: "absolute", ButtonState: &one})
	s.releaseHeldMouse(old, "synthetic retired peer")
	if !s.mouseHeldBy(next) {
		t.Fatal("old peer released new hold")
	}
	s.releaseHeldMouse(next, "synthetic current peer")
	if s.mouseHeldBy(next) {
		t.Fatal("current hold survived retirement")
	}
}
