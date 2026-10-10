package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"net/http/httptest"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"usbridge-client/internal/sourcepreview"
)

func previewTestDescriptor(t *testing.T) *sourcepreview.Descriptor {
	t.Helper()
	now := time.Now()
	b, err := json.Marshal(map[string]any{
		"schema_version": 1, "profile": sourcepreview.Profile, "session_id": "preview_test_0123456789",
		"rtsp_url": "rtspenc://127.0.0.1:45678", "key_b64": base64.StdEncoding.EncodeToString(make([]byte, 16)), "key_id": uint32(0xfedcba98),
		"width": sourcepreview.DefaultWidth, "height": sourcepreview.DefaultHeight, "fps": 30, "bitrate_kbps": 1000, "expires_at": now.Add(20 * time.Second),
	})
	if err != nil {
		t.Fatal(err)
	}
	d, err := sourcepreview.Decode(b, now)
	if err != nil {
		t.Fatal(err)
	}
	return d
}

func TestSourcePreviewNeverPairsOrReconnects(t *testing.T) {
	m := NewSourcePreviewService()
	if m.client != nil {
		t.Fatal("source constructor initialized a stock HTTP client")
	}
	if err := m.ConnectToMoonlight(); err == nil {
		t.Fatal("source service entered stock connection")
	}
	if err := m.Reconnect(); err == nil {
		t.Fatal("source service entered stock reconnect")
	}
	if err := m.Disconnect(); err != nil {
		t.Fatal(err)
	}
}
func TestSourcePreviewRejectedLeaseReleasesOnce(t *testing.T) {
	m := NewSourcePreviewService()
	d := previewTestDescriptor(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	var releases atomic.Int32
	if err := m.ConnectToSourcePreview(ctx, d, func() { releases.Add(1) }); err == nil {
		t.Fatal("canceled lease accepted")
	}
	if releases.Load() != 1 {
		t.Fatal("lease release count", releases.Load())
	}
	if _, err := d.Consume(time.Now()); err == nil {
		t.Fatal("rejected descriptor retained key")
	}
	_ = m.Disconnect()
	if releases.Load() != 1 {
		t.Fatal("disconnect repeated rejected release")
	}
}
func TestSourcePreviewBusyDoesNotReplaceSession(t *testing.T) {
	if !sourcePreviewSupported() {
		t.Skip("native Linux or Windows source preview only")
	}
	m := NewSourcePreviewService()
	m.isRunning = true
	d := previewTestDescriptor(t)
	var releases int
	if err := m.ConnectToSourcePreview(context.Background(), d, func() { releases++ }); err == nil {
		t.Fatal("busy renderer replaced")
	}
	if !m.isRunning || m.sourcePreview != nil || releases != 1 {
		t.Fatal("busy session mutated")
	}
	if _, err := d.Consume(time.Now()); err == nil {
		t.Fatal("busy descriptor reusable")
	}
}
func TestSourcePreviewTerminalGenerationAndRelease(t *testing.T) {
	m := NewSourcePreviewService()
	ctx, cancel := context.WithCancel(context.Background())
	stop := make(chan struct{})
	var releases atomic.Int32
	m.isRunning = true
	m.stopPlayerCh = stop
	m.sourcePreview = &sourcePreviewState{generation: 2, cancel: cancel, release: func() { releases.Add(1) }}
	m.finishSourcePreview(1)
	select {
	case <-ctx.Done():
		t.Fatal("stale callback canceled current lease")
	default:
	}
	if releases.Load() != 0 || !m.isRunning {
		t.Fatal("stale callback modified active state")
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() { m.finishSourcePreview(2) })
	}
	wg.Wait()
	if releases.Load() != 1 || m.isRunning || m.sourcePreview != nil {
		t.Fatal("terminal state/release mismatch")
	}
	select {
	case <-stop:
	default:
		t.Fatal("decoder stop not closed")
	}
	select {
	case <-ctx.Done():
	default:
		t.Fatal("lease context not canceled")
	}
}
func TestSourcePreviewStaleDisconnectDoesNotClearReadyHandler(t *testing.T) {
	m := NewSourcePreviewService()
	m.sourcePreview = &sourcePreviewState{generation: 2}
	var ready atomic.Int32
	setMoonlightStreamReadyHandler(func() { ready.Add(1) })
	defer clearMoonlightStreamReadyHandler()
	_ = m.disconnect(1)
	notifyMoonlightStreamReady()
	if ready.Load() != 1 {
		t.Fatal("stale disconnect cleared newer ready handler")
	}
}

// The one-shot guard makes a regression fail with a bounded wait instead of
// overflowing the stack if a terminal callback recursively emits another event.
func TestSourcePreviewDisconnectCallbackCanReenterSynchronously(t *testing.T) {
	m := NewSourcePreviewService()
	m.isRunning = true
	stop := make(chan struct{})
	m.stopPlayerCh = stop
	var calls atomic.Int32
	var reentered atomic.Bool
	callbackFailure := make(chan string, 4)
	m.SetOnStateChanged(func(state string) {
		if state != "disconnected" {
			callbackFailure <- "unexpected state"
			return
		}
		calls.Add(1)
		m.mu.Lock()
		incomplete := !m.disconnecting || !m.disconnectTeardownComplete || m.disconnectDone == nil
		m.mu.Unlock()
		if incomplete {
			callbackFailure <- "notification preceded teardown completion"
		}
		select {
		case <-stop:
		default:
			callbackFailure <- "notification preceded decoder teardown"
		}
		if reentered.CompareAndSwap(false, true) {
			if err := m.Disconnect(); err != nil {
				callbackFailure <- "reentrant disconnect failed"
			}
		}
	})
	done := make(chan struct{})
	go func() { defer close(done); _ = m.Disconnect() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal callback deadlocked while re-entering Disconnect")
	}
	select {
	case message := <-callbackFailure:
		t.Fatal(message)
	default:
	}
	if calls.Load() != 1 {
		t.Fatalf("reentrant callback emitted %d terminal notifications, want 1", calls.Load())
	}
}

func TestSourcePreviewCompletedDisconnectIsQuiet(t *testing.T) {
	m := NewSourcePreviewService()
	m.isRunning = true
	var calls atomic.Int32
	m.SetOnStateChanged(func(state string) {
		if state == "disconnected" {
			calls.Add(1)
		}
	})
	if err := m.Disconnect(); err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 32 {
		wg.Go(func() { _ = m.Disconnect() })
	}
	wg.Wait()
	if calls.Load() != 1 {
		t.Fatalf("completed disconnect emitted %d terminal notifications, want 1", calls.Load())
	}
}

func TestDisconnectPublishesCompletionAfterStockCancel(t *testing.T) {
	host := &fakeSunshine{cancelDelay: 25 * time.Millisecond}
	server := httptest.NewTLSServer(host.handler())
	defer server.Close()
	client := newTestMoonlightClient(t, server)
	if _, _, err := client.Launch(1, "h264", 128, 72, 30, 1000); err != nil {
		t.Fatal(err)
	}
	m := &MoonlightService{client: client, lastAppId: 1}
	var calls atomic.Int32
	var reentered atomic.Bool
	callbackFailure := make(chan string, 4)
	m.SetOnStateChanged(func(state string) {
		if state != "disconnected" {
			return
		}
		calls.Add(1)
		running, _, _, _, cancels := host.snapshot()
		if running || cancels != 1 {
			callbackFailure <- "terminal notification preceded stock cancel"
		}
		m.mu.Lock()
		incomplete := !m.disconnecting || !m.disconnectTeardownComplete || m.disconnectDone == nil
		m.mu.Unlock()
		if incomplete {
			callbackFailure <- "terminal notification preceded completion publication"
		}
		if reentered.CompareAndSwap(false, true) {
			_ = m.Disconnect()
		}
	})
	done := make(chan struct{})
	go func() { defer close(done); _ = m.Disconnect() }()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("stock terminal callback deadlocked")
	}
	select {
	case message := <-callbackFailure:
		t.Fatal(message)
	default:
	}
	if calls.Load() != 1 {
		t.Fatalf("stock disconnect emitted %d terminal notifications, want 1", calls.Load())
	}
}

// Hold the old notification at a deterministic boundary. A second Disconnect
// must return quietly, but must not admit a new connection before that old
// notification returns. The nil HTTP client ensures a broken admission guard
// cannot accidentally contact a real host; reaching it is a caught test failure.
func TestDisconnectBlocksNewConnectionUntilTerminalNotificationReturns(t *testing.T) {
	m := &MoonlightService{isRunning: true, serverHost: "127.0.0.1"}
	entered := make(chan struct{})
	resume := make(chan struct{})
	done := make(chan struct{})
	var allowOnce sync.Once
	allowNotification := func() { allowOnce.Do(func() { close(resume) }) }
	defer allowNotification()
	m.SetOnStateChanged(func(state string) {
		if state == "disconnected" {
			close(entered)
			<-resume
		}
	})
	go func() { defer close(done); _ = m.Disconnect() }()
	select {
	case <-entered:
	case <-time.After(2 * time.Second):
		t.Fatal("terminal callback was not reached")
	}
	m.mu.Lock()
	join := m.disconnectDone
	gated := m.disconnecting && m.disconnectTeardownComplete && join != nil
	m.mu.Unlock()
	if !gated {
		t.Error("connection admission released before terminal notification returned")
	}
	select {
	case <-join:
		t.Error("earlier disconnect waiters released before terminal notification")
	default:
	}

	type result struct {
		err      error
		admitted bool
	}
	attempt := make(chan result, 1)
	go func() {
		defer func() {
			if recover() != nil {
				attempt <- result{admitted: true}
			}
		}()
		_ = m.Disconnect() // Quiet re-entry after resource teardown.
		err := m.ConnectToMoonlight()
		attempt <- result{err: err, admitted: m.connGen.Load() != 0}
	}()
	select {
	case result := <-attempt:
		if result.admitted || result.err == nil {
			t.Fatal("new connection admitted during old terminal notification")
		}
	case <-time.After(2 * time.Second):
		t.Fatal("reentrant disconnect waited on its own terminal notification")
	}
	allowNotification()
	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("disconnect did not finish after notification returned")
	}
	select {
	case <-join:
	default:
		t.Fatal("disconnect join channel remained open")
	}
	m.mu.Lock()
	stillGated := m.disconnecting || m.disconnectTeardownComplete || m.disconnectDone != nil
	m.mu.Unlock()
	if stillGated {
		t.Fatal("connection admission was not released after notification")
	}
}
