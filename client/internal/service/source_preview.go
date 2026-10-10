package service

import (
	"context"
	"fmt"
	"os"
	"sync"
	"time"

	"usbridge-client/internal/models"
	"usbridge-client/internal/sourcepreview"
)

type sourcePreviewState struct {
	generation uint64
	cancel     context.CancelFunc
	release    func()
}

// NewSourcePreviewService builds a renderer without loading, generating or
// persisting a GameStream identity. It cannot use stock pairing/reconnect.
func NewSourcePreviewService() *MoonlightService {
	return &MoonlightService{config: &models.AppConfig{}, sourcePreviewOnly: true}
}

// ConnectToSourcePreview consumes a private descriptor issued after local host
// capture approval. ctx is the parent's lease (EOF/cancel closes it). release
// must be nonblocking and is called exactly once on every terminal path. This
// method performs no discovery, pairing, HTTP launch, remote relay or retry.
func (m *MoonlightService) ConnectToSourcePreview(ctx context.Context, descriptor *sourcepreview.Descriptor, release func()) (err error) {
	var releaseOnce sync.Once
	releaseLease := func() {
		releaseOnce.Do(func() {
			if release != nil {
				release()
			}
		})
	}
	accepted := false
	defer func() {
		if !accepted {
			releaseLease()
		}
	}()
	if !sourcePreviewSupported() {
		if descriptor != nil {
			descriptor.Destroy()
		}
		return fmt.Errorf("source preview requires a native Linux client")
	}
	if ctx == nil {
		if descriptor != nil {
			descriptor.Destroy()
		}
		return fmt.Errorf("source preview requires a parent lease")
	}
	if err := ctx.Err(); err != nil {
		if descriptor != nil {
			descriptor.Destroy()
		}
		return fmt.Errorf("source preview lease ended")
	}

	m.mu.Lock()
	if m.connecting || m.disconnecting || m.isRunning || m.activeWrapper != nil || m.sourcePreview != nil || m.lastAppId != 0 || m.sourcePreviewUsed {
		m.mu.Unlock()
		if descriptor != nil {
			descriptor.Destroy()
		}
		return fmt.Errorf("renderer is busy or its source preview has already been used")
	}
	c, err := descriptor.Consume(time.Now())
	if err != nil {
		m.mu.Unlock()
		return err
	}
	defer clear(c.Key[:])
	lease, cancel := context.WithDeadline(ctx, descriptor.ExpiresAt())
	abort := make(chan struct{})
	gen := m.connGen.Add(1)
	m.connecting = true
	m.isRunning = true
	m.sourcePreviewOnly = true
	m.sourcePreviewUsed = true
	m.serverHost = "127.0.0.1"
	m.abort = abort
	m.sourcePreview = &sourcePreviewState{generation: gen, cancel: cancel, release: releaseLease}
	m.mu.Unlock()
	defer func() {
		m.mu.Lock()
		m.connecting = false
		m.mu.Unlock()
		if err != nil {
			_ = m.Disconnect()
		}
	}()

	// Cancel even if native setup is still in flight. Disconnect uses the same
	// interrupt-before-join protection as the normal Moonlight renderer.
	go func() {
		<-lease.Done()
		m.mu.Lock()
		current := m.sourcePreview != nil && m.sourcePreview.generation == gen
		m.mu.Unlock()
		if current {
			_ = m.disconnect(gen)
		}
	}()
	err = m.startMoonlightRenderer(moonlightRendererParameters{
		host: "127.0.0.1", sessionURL: c.RTSPURL, key: c.Key[:],
		appVersion: sourcepreview.AppVersion, serverCodecModeSupport: sourcepreview.ServerCodecModeSupport,
		width: c.Width, height: c.Height, fps: c.FPS, bitrate: c.BitrateKbps,
		source: &sourcePreviewNativeConfig{keyID: c.KeyID},
	}, gen, abort, time.Now())
	accepted = err == nil
	return err
}

// finishSourcePreview cannot release or cancel a newer generation. Native
// failure, context cancellation and UI disconnect may race here safely.
func (m *MoonlightService) finishSourcePreview(gen uint64) {
	m.mu.Lock()
	state := m.sourcePreview
	if state == nil || state.generation != gen {
		m.mu.Unlock()
		return
	}
	m.sourcePreview = nil
	m.isRunning = false
	m.activeWrapper = nil
	stop := m.stopPlayerCh
	m.stopPlayerCh = nil
	m.mu.Unlock()
	if stop != nil {
		close(stop)
	}
	state.cancel()
	state.release()
}

type sourcePreviewNativeConfig struct{ keyID uint32 }
type moonlightStartStream func(string, []byte, string, string, int, int, int, int, int, int, *os.File, *os.File, func(error)) error
