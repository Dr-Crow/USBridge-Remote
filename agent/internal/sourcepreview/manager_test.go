package sourcepreview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/sourcestreamer"
)

type fakeProcess struct {
	done    chan struct{}
	once    sync.Once
	first   chan struct{}
	stopped func()
	err     error
}

func fake() *fakeProcess                     { return &fakeProcess{done: make(chan struct{}), first: make(chan struct{})} }
func (f *fakeProcess) Done() <-chan struct{} { return f.done }
func (f *fakeProcess) Wait() error           { <-f.done; return f.err }
func (f *fakeProcess) Stop() error {
	f.once.Do(func() {
		if f.stopped != nil {
			f.stopped()
		}
		close(f.done)
	})
	return f.err
}
func (f *fakeProcess) FirstFrame() <-chan struct{} { return f.first }
func (f *fakeProcess) Address() string             { return "127.0.0.1:40000" }
func approval() Approval {
	return Approval{Components: localcomponents.Options{Directory: "/components", StateDir: "/state", ManifestSHA256: strings.Repeat("a", 64)}, Display: ":99", FFmpeg: "/usr/bin/ffmpeg", CaptureConsent: true}
}
func fixture(t *testing.T) (*Manager, *fakeProcess, *fakeProcess) {
	t.Helper()
	if runtime.GOOS != "linux" {
		t.Skip("Linux-only preview manager")
	}
	s, v := fake(), fake()
	m := &Manager{deps: dependencies{random: bytes.NewReader(bytes.Repeat([]byte{1, 2, 3, 4, 5, 6, 7}, 100)), now: time.Now, permitted: func() bool { return true }, prepareViewer: func(context.Context, localcomponents.Options) (string, error) { return "/viewer", nil }, startStream: func(context.Context, localcomponents.Options, sourcestreamer.Launch) (stream, error) { return s, nil }, startViewer: func(context.Context, string, descriptor) (viewer, error) { return v, nil }}}
	return m, s, v
}
func TestPreviewRequiresLocalConsentAndPackage(t *testing.T) {
	for _, change := range []func(*Approval){func(a *Approval) { a.CaptureConsent = false }, func(a *Approval) { a.Components.ManifestSHA256 = "" }, func(a *Approval) { a.Components.Mirror = "https://remote.invalid" }, func(a *Approval) { a.Display = "localhost:0" }, func(a *Approval) { a.FFmpeg = "ffmpeg" }} {
		m, _, _ := fixture(t)
		a := approval()
		change(&a)
		if _, err := m.Start(context.Background(), a); err == nil {
			t.Fatal("unsafe approval accepted")
		}
	}
	m, _, _ := fixture(t)
	m.deps.permitted = func() bool { return false }
	if _, err := m.Start(context.Background(), approval()); err == nil {
		t.Fatal("privileged/unsupported start accepted")
	}
}
func TestPreviewSingleReservationAndOrderedJoinedStop(t *testing.T) {
	m, source, view := fixture(t)
	order := make(chan string, 2)
	source.stopped = func() { order <- "source" }
	view.stopped = func() { order <- "viewer" }
	s, err := m.Start(context.Background(), approval())
	if err != nil {
		t.Fatal(err)
	}
	if s.State() != "connecting" {
		t.Fatal(s.State())
	}
	if _, err := m.Start(context.Background(), approval()); err == nil {
		t.Fatal("overlapping launch")
	}
	close(view.first)
	deadline := time.Now().Add(time.Second)
	for s.State() != "viewing" && time.Now().Before(deadline) {
		time.Sleep(time.Millisecond)
	}
	if s.State() != "viewing" {
		t.Fatal("no actual frame state")
	}
	if err := s.Stop(); err != nil {
		t.Fatal(err)
	}
	if <-order != "viewer" || <-order != "source" {
		t.Fatal("teardown order")
	}
	if s.State() != "stopped" {
		t.Fatal(s.State())
	}
}
func TestPreviewFreshSecretAndNoDescriptorInStatus(t *testing.T) {
	m, _, _ := fixture(t)
	var keys []string
	var ids []string
	m.deps.startStream = func(_ context.Context, _ localcomponents.Options, l sourcestreamer.Launch) (stream, error) {
		if l.InputConsent || l.AudioMode != "silence" || l.Width != 128 || l.Height != 72 || l.FPS != 30 || l.MaxSeconds != 30 || l.PacketSize != 1024 || l.VideoPort != 0 || l.AudioPort != 0 {
			t.Error("unexpected grant")
		}
		return fake(), nil
	}
	m.deps.startViewer = func(_ context.Context, _ string, d descriptor) (viewer, error) {
		if d.SchemaVersion != 1 || d.Profile != Profile || !strings.HasPrefix(d.RTSPURL, "rtspenc://127.0.0.1:") {
			t.Error("bad descriptor")
		}
		if strings.Contains(fmt.Sprintf("%+v %#v", d, d), d.KeyB64) {
			t.Error("secret formatted")
		}
		keys = append(keys, d.KeyB64)
		ids = append(ids, d.SessionID)
		return fake(), nil
	}
	for i := 0; i < 2; i++ {
		s, e := m.Start(context.Background(), approval())
		if e != nil {
			t.Fatal(e)
		}
		if e = s.Stop(); e != nil {
			t.Fatal(e)
		}
	}
	if keys[0] == keys[1] || ids[0] == ids[1] {
		t.Fatal("reused session material")
	}
}
func TestPreviewRandomFailureDoesNotLaunchOrHoldReservation(t *testing.T) {
	m, _, _ := fixture(t)
	m.deps.random = bytes.NewReader(nil)
	m.deps.prepareViewer = func(context.Context, localcomponents.Options) (string, error) {
		t.Fatal("prepared after random failed")
		return "", nil
	}
	for i := 0; i < 2; i++ {
		if _, err := m.Start(context.Background(), approval()); err == nil || strings.Contains(err.Error(), "already active") {
			t.Fatal(err)
		}
	}
}
func TestPreviewViewerFailureJoinsSource(t *testing.T) {
	m, source, _ := fixture(t)
	m.deps.startViewer = func(context.Context, string, descriptor) (viewer, error) {
		return nil, errors.New("possibly sensitive backend failure")
	}
	if _, err := m.Start(context.Background(), approval()); err == nil || strings.Contains(err.Error(), "sensitive") {
		t.Fatal(err)
	}
	select {
	case <-source.Done():
	default:
		t.Fatal("source leaked")
	}
}
func TestPreviewCancelDuringStartupReleasesReservation(t *testing.T) {
	m, _, _ := fixture(t)
	entered := make(chan struct{})
	m.deps.startStream = func(ctx context.Context, _ localcomponents.Options, _ sourcestreamer.Launch) (stream, error) {
		close(entered)
		<-ctx.Done()
		return nil, ctx.Err()
	}
	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { _, e := m.Start(ctx, approval()); done <- e }()
	<-entered
	cancel()
	select {
	case e := <-done:
		if e == nil {
			t.Fatal("canceled startup succeeded")
		}
	case <-time.After(time.Second):
		t.Fatal("startup cancellation did not join")
	}
	m.mu.Lock()
	active := m.active
	m.mu.Unlock()
	if active != nil {
		t.Fatal("reservation leaked")
	}
}
func TestPreviewParentCancelJoinsBoth(t *testing.T) {
	m, source, view := fixture(t)
	ctx, cancel := context.WithCancel(context.Background())
	s, err := m.Start(ctx, approval())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	select {
	case <-s.Done():
	case <-time.After(time.Second):
		t.Fatal("cancel stuck")
	}
	for _, p := range []*fakeProcess{source, view} {
		select {
		case <-p.Done():
		default:
			t.Fatal("child leaked")
		}
	}
}

func TestPreviewExposesOnlyTypedFrameLimit(t *testing.T) {
	m, source, _ := fixture(t)
	source.err = sourcestreamer.ErrFrameTooLarge
	session, err := m.Start(context.Background(), approval())
	if err != nil {
		t.Fatal(err)
	}
	_ = source.Stop()
	if err = session.Wait(); !errors.Is(err, ErrFrameTooLarge) {
		t.Fatalf("typed limit missing: %v", err)
	}
}
