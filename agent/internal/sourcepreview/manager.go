// Package sourcepreview owns a locally approved, one-shot source preview.
// It has no network or admin-socket request handler. Only an explicit local UI
// approval may call Start; neither child may choose its capture scope or keys.
package sourcepreview

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"errors"
	"io"
	"os"
	"runtime"
	"sync"
	"time"

	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/sourcestreamer"
)

const Profile = "source-preview-v1"

var ErrFrameTooLarge = errors.New("capture content exceeds this preview profile; select the smaller profile or a verified bounded encoder")

// Approval is constructed by the local consent dialog, never by a remote API.
// Version one defaults to view-only 128x72/30fps with synthesized silence for up to 30 seconds.
type Approval struct {
	Components     localcomponents.Options
	Display        string
	FFmpeg         string
	CaptureConsent bool
	VideoProfile   string
}

type descriptor struct {
	SchemaVersion int       `json:"schema_version"`
	Profile       string    `json:"profile"`
	SessionID     string    `json:"session_id"`
	RTSPURL       string    `json:"rtsp_url"`
	KeyB64        string    `json:"key_b64"`
	KeyID         uint32    `json:"key_id"`
	Width         int       `json:"width"`
	Height        int       `json:"height"`
	FPS           int       `json:"fps"`
	BitrateKbps   int       `json:"bitrate_kbps"`
	ExpiresAt     time.Time `json:"expires_at"`
}

func (descriptor) String() string   { return "source-preview descriptor [redacted]" }
func (descriptor) GoString() string { return "source-preview descriptor [redacted]" }

type process interface {
	Done() <-chan struct{}
	Wait() error
	Stop() error
}
type stream interface {
	process
	Address() string
}
type viewer interface {
	process
	FirstFrame() <-chan struct{}
}
type streamSession struct{ *sourcestreamer.Session }

func (s streamSession) Address() string { return s.Ready.RTSPAddress }

type dependencies struct {
	random        io.Reader
	now           func() time.Time
	permitted     func() bool
	prepareViewer func(context.Context, localcomponents.Options) (string, error)
	startStream   func(context.Context, localcomponents.Options, sourcestreamer.Launch) (stream, error)
	startViewer   func(context.Context, string, descriptor) (viewer, error)
}

// Manager does not persist grants, keys, launch descriptors, or previous sessions.
// A single active reservation includes startup and teardown, preventing double-clicks
// or delayed callbacks from starting overlapping capture children.
type Manager struct {
	mu     sync.Mutex
	active *Session
	deps   dependencies
}

func New() *Manager {
	return &Manager{deps: dependencies{random: rand.Reader, now: time.Now,
		permitted:     func() bool { return runtime.GOOS == "linux" && os.Geteuid() != 0 },
		prepareViewer: prepareViewer,
		startStream: func(ctx context.Context, o localcomponents.Options, l sourcestreamer.Launch) (stream, error) {
			s, e := sourcestreamer.Start(ctx, o, l)
			if e != nil {
				return nil, e
			}
			return streamSession{s}, nil
		},
		startViewer: startViewer,
	}}
}

// Stop joins any startup or active preview owned by this manager. It is safe
// when no preview exists and cannot cancel a subsequent session generation.
func (m *Manager) Stop() error {
	m.mu.Lock()
	s := m.active
	m.mu.Unlock()
	if s == nil {
		return nil
	}
	return s.Stop()
}

// Session reports UI-safe status only. A ready child is "connecting"; the UI
// must receive a real first-frame signal before showing "viewing".
type Session struct {
	id     string
	cancel context.CancelFunc
	done   chan struct{}
	mu     sync.Mutex
	phase  string
	err    error
}

func (s *Session) ID() string            { return s.id }
func (s *Session) Done() <-chan struct{} { return s.done }
func (s *Session) State() string         { s.mu.Lock(); defer s.mu.Unlock(); return s.phase }
func (s *Session) Wait() error           { <-s.done; s.mu.Lock(); defer s.mu.Unlock(); return s.err }
func (s *Session) Stop() error           { s.cancel(); return s.Wait() }
func (s *Session) set(phase string, err error) {
	s.mu.Lock()
	s.phase = phase
	s.err = err
	s.mu.Unlock()
}

func (m *Manager) Start(ctx context.Context, a Approval) (*Session, error) {
	if !m.deps.permitted() {
		return nil, errors.New("source preview requires an unprivileged same-user Linux GUI")
	}
	if !a.CaptureConsent {
		return nil, errors.New("local capture approval is required")
	}
	if a.Components.Directory == "" || a.Components.ManifestSHA256 == "" || a.Components.Bundle != "" || a.Components.Mirror != "" {
		return nil, errors.New("source preview requires a local hash-pinned component directory")
	}
	width, height := 128, 72
	switch a.VideoProfile {
	case "", "transport-128":
	case "desktop-640":
		width, height = 640, 360
	default:
		return nil, errors.New("unsupported preview profile")
	}
	launch := sourcestreamer.Launch{SchemaVersion: 1, Owner: "local-preview", SessionID: "validation", KeyB64: base64.StdEncoding.EncodeToString(make([]byte, 16)), PeerIP: "127.0.0.1", Display: a.Display, CaptureConsent: true, FFmpeg: a.FFmpeg, Width: width, Height: height, FPS: 30, PixelFormat: "yuv420p", PacketSize: 1024, AudioMode: "silence", MaxSeconds: 30}
	if err := launch.Validate(); err != nil {
		return nil, err
	}
	sessionCtx, cancel := context.WithCancel(ctx)
	childCtx, cancelChildren := context.WithCancel(context.WithoutCancel(ctx))
	startupDone := make(chan struct{})
	go func() {
		select {
		case <-sessionCtx.Done():
			select {
			case <-startupDone:
			default:
				cancelChildren()
			}
		case <-startupDone:
		}
	}()
	s := &Session{cancel: cancel, done: make(chan struct{}), phase: "starting"}
	m.mu.Lock()
	if m.active != nil {
		m.mu.Unlock()
		cancel()
		cancelChildren()
		return nil, errors.New("a source preview is already active")
	}
	m.active = s
	m.mu.Unlock()
	finish := func(err error) {
		cancelChildren()
		cancel()
		s.set("stopped", err)
		m.mu.Lock()
		if m.active == s {
			m.active = nil
		}
		m.mu.Unlock()
		close(s.done)
	}
	fail := func(err error) (*Session, error) { finish(err); return nil, err }
	secret := make([]byte, 36)
	if _, err := io.ReadFull(m.deps.random, secret); err != nil {
		return fail(errors.New("cannot create a fresh preview session"))
	}
	s.id = hex.EncodeToString(secret[20:])
	launch.SessionID = s.id
	launch.KeyB64 = base64.StdEncoding.EncodeToString(secret[:16])
	launch.KeyID = binary.BigEndian.Uint32(secret[16:20])
	for i := range secret {
		secret[i] = 0
	}
	binaryPath, err := m.deps.prepareViewer(sessionCtx, a.Components)
	if err != nil {
		return fail(errors.New("verified preview viewer is unavailable"))
	}
	child, err := m.deps.startStream(childCtx, a.Components, launch)
	if err != nil {
		return fail(errors.New("source preview could not start"))
	}
	// Source readiness is validated by the supervisor. The viewer independently
	// validates its loopback URL and expiry before accepting the secret descriptor.
	d := descriptor{SchemaVersion: 1, Profile: Profile, SessionID: s.id, RTSPURL: "rtspenc://" + child.Address(), KeyB64: launch.KeyB64, KeyID: launch.KeyID, Width: width, Height: height, FPS: 30, BitrateKbps: 10000, ExpiresAt: m.deps.now().Add(28 * time.Second)}
	v, err := m.deps.startViewer(childCtx, binaryPath, d)
	if err != nil {
		_ = child.Stop()
		return fail(errors.New("preview renderer could not start"))
	}
	close(startupDone)
	s.set("connecting", nil)
	go func() {
		// Leave two seconds inside the source's 30-second cap for native
		// encrypted disconnect before its independent hard deadline.
		timer := time.NewTimer(time.Until(d.ExpiresAt))
		defer timer.Stop()
		var terminal error
		select {
		case <-v.FirstFrame():
			s.set("viewing", nil)
		case <-v.Done():
			terminal = v.Wait()
		case <-child.Done():
			terminal = child.Wait()
		case <-sessionCtx.Done():
		case <-timer.C:
		}
		if s.State() == "viewing" {
			select {
			case <-v.Done():
				terminal = v.Wait()
			case <-child.Done():
				terminal = child.Wait()
			case <-sessionCtx.Done():
			case <-timer.C:
			}
		}
		// Stop the renderer first so its native client can perform a normal encrypted
		// teardown while the source sockets still exist, then join the capture child.
		if err := v.Stop(); terminal == nil && err != nil {
			terminal = errors.New("preview renderer did not stop cleanly")
		}
		if err := child.Stop(); errors.Is(err, sourcestreamer.ErrFrameTooLarge) {
			terminal = ErrFrameTooLarge
		} else if terminal == nil && err != nil {
			terminal = errors.New("source preview did not stop cleanly")
		}
		if terminal != nil && !errors.Is(terminal, ErrFrameTooLarge) {
			terminal = errors.New("source preview ended unsuccessfully")
		}
		finish(terminal)
	}()
	return s, nil
}
