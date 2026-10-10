package service

import (
	"context"
	"encoding/base64"
	"encoding/json"
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
		t.Skip("native Linux source preview only")
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
