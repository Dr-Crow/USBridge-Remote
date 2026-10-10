package sourcepreview

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/previewprocess"
	"usbridge_agent/internal/sourcestreamer"
)

// Wait and Stop deliberately report independent results. A timed-out Stop may
// return while Done remains open, so manager completion cannot prove retirement.
type leaseProcess struct {
	done, first      chan struct{}
	once             sync.Once
	waitErr, stopErr error
	keepRunning      bool
	onStop           func()
	stops            atomic.Int32
}

func newLeaseProcess() *leaseProcess {
	return &leaseProcess{done: make(chan struct{}), first: make(chan struct{})}
}
func (p *leaseProcess) Done() <-chan struct{}       { return p.done }
func (p *leaseProcess) FirstFrame() <-chan struct{} { return p.first }
func (p *leaseProcess) Address() string             { return "127.0.0.1:40000" }
func (p *leaseProcess) retire()                     { p.once.Do(func() { close(p.done) }) }
func (p *leaseProcess) Wait() error                 { <-p.done; return p.waitErr }
func (p *leaseProcess) Stop() error {
	p.stops.Add(1)
	if p.onStop != nil {
		p.onStop()
	}
	if !p.keepRunning {
		p.retire()
	}
	return p.stopErr
}

// These policy tests use only Go fakes. Platform-appropriate approval fields
// exercise validation on native Windows without enabling or starting a preview.
func leaseApproval() Approval {
	a := Approval{Components: localcomponents.Options{Directory: "/components", StateDir: "/state", ManifestSHA256: strings.Repeat("a", 64)}, Display: ":99", FFmpeg: "/usr/bin/ffmpeg", CaptureConsent: true}
	if runtime.GOOS == "windows" {
		a.Components.Directory, a.Components.StateDir = `C:\preview-fixtures\components`, `C:\preview-fixtures\state`
		a.Display, a.FFmpeg = "desktop", `C:\preview-fixtures\ffmpeg.exe`
	}
	return a
}

func leaseFixture(t *testing.T) (*Manager, *leaseProcess, *leaseProcess) {
	t.Helper()
	source, view := newLeaseProcess(), newLeaseProcess()
	m := &Manager{deps: dependencies{
		random:        bytes.NewReader(bytes.Repeat([]byte{1, 2, 3, 4, 5, 6, 7}, 100)),
		now:           time.Now,
		permitted:     func() bool { return true },
		prepareViewer: func(context.Context, localcomponents.Options) (string, error) { return "synthetic-viewer", nil },
	}}
	m.deps.startStream = func(context.Context, localcomponents.Options, sourcestreamer.Launch) (stream, error) {
		return source, nil
	}
	m.deps.startViewer = func(context.Context, string, descriptor) (viewer, error) { return view, nil }
	return m, source, view
}

func awaitLeaseResult(t *testing.T, result <-chan error) error {
	t.Helper()
	select {
	case err := <-result:
		return err
	case <-time.After(3 * time.Second):
		t.Fatal("manager operation did not join")
		return nil
	}
}

func assertLeaseRetained(t *testing.T, m *Manager) {
	t.Helper()
	m.mu.Lock()
	active, blocked := m.active, m.cleanupUncertain
	m.mu.Unlock()
	if active == nil || !blocked {
		t.Fatal("uncertain cleanup released the manager reservation")
	}
	if active.State() != "stopped" || active.Wait() != ErrCleanupUncertain {
		t.Fatal("retained session did not finish with safe cleanup status")
	}
	// No fresh preparation, key generation, or launch may occur after poison.
	m.deps.random = nil
	m.deps.permitted = func() bool { t.Error("rechecked platform after poison"); return true }
	m.deps.prepareViewer = func(context.Context, localcomponents.Options) (string, error) {
		t.Error("prepared a viewer after poison")
		return "", errors.New("unexpected preparation")
	}
	m.deps.startStream = func(context.Context, localcomponents.Options, sourcestreamer.Launch) (stream, error) {
		t.Error("launched a source after poison")
		return nil, errors.New("unexpected launch")
	}
	m.deps.startViewer = func(context.Context, string, descriptor) (viewer, error) {
		t.Error("launched a viewer after poison")
		return nil, errors.New("unexpected launch")
	}
	for i := 0; i < 3; i++ {
		if err := m.Stop(); err != ErrCleanupUncertain {
			t.Fatalf("repeated Stop lost the block: %v", err)
		}
		for _, a := range []Approval{leaseApproval(), {}} {
			if session, err := m.Start(context.Background(), a); session != nil || err != ErrCleanupUncertain {
				t.Fatalf("fresh attempt did not return the safe block: %v", err)
			}
		}
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.active != active || !m.cleanupUncertain {
		t.Fatal("repeated Stop/Start replaced or released the retained lease")
	}
}

func TestPreviewLeaseSourceStartupUncertainty(t *testing.T) {
	for _, cause := range []error{previewprocess.ErrCleanupTimeout, previewprocess.ErrContainment} {
		t.Run(cause.Error(), func(t *testing.T) {
			m, _, _ := leaseFixture(t)
			var key string
			m.deps.startStream = func(_ context.Context, _ localcomponents.Options, launch sourcestreamer.Launch) (stream, error) {
				key = launch.KeyB64
				return nil, fmt.Errorf("private child payload %s: %w", key, errors.Join(cause, sourcestreamer.ErrFrameTooLarge))
			}
			m.deps.startViewer = func(context.Context, string, descriptor) (viewer, error) {
				t.Fatal("viewer launched after failed source startup")
				return nil, nil
			}
			_, err := m.Start(context.Background(), leaseApproval())
			if err != ErrCleanupUncertain || strings.Contains(fmt.Sprintf("%+v %#v", err, err), key) {
				t.Fatalf("startup lost cleanup uncertainty or disclosed a key: %v", err)
			}
			assertLeaseRetained(t, m)
		})
	}
}

func TestPreviewLeaseViewerStartupCombinesSourceCleanup(t *testing.T) {
	for _, tc := range []struct {
		name      string
		viewerErr error
		sourceErr error
	}{
		{"viewer-timeout", previewprocess.ErrCleanupTimeout, nil},
		{"viewer-containment-source-frame", previewprocess.ErrContainment, sourcestreamer.ErrFrameTooLarge},
		{"source-timeout", errors.New("private renderer failure"), previewprocess.ErrCleanupTimeout},
		{"source-containment-viewer-frame", sourcestreamer.ErrFrameTooLarge, previewprocess.ErrContainment},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, source, _ := leaseFixture(t)
			source.stopErr, source.keepRunning = tc.sourceErr, tc.sourceErr != nil
			m.deps.startViewer = func(_ context.Context, _ string, d descriptor) (viewer, error) {
				return nil, fmt.Errorf("private viewer key %s: %w", d.KeyB64, tc.viewerErr)
			}
			if _, err := m.Start(context.Background(), leaseApproval()); err != ErrCleanupUncertain {
				t.Fatalf("combined startup errors lost uncertainty: %v", err)
			}
			if source.stops.Load() != 1 {
				t.Fatal("source was not stopped after viewer startup failure")
			}
			assertLeaseRetained(t, m)
		})
	}
}

func TestPreviewLeaseTerminalUncertaintyOutranksOtherErrors(t *testing.T) {
	for _, tc := range []struct {
		name                     string
		terminalOnSource         bool
		terminal, source, viewer error
	}{
		{"source-terminal-timeout", true, previewprocess.ErrCleanupTimeout, nil, nil},
		{"viewer-terminal-containment", false, previewprocess.ErrContainment, nil, nil},
		{"viewer-timeout-source-frame", false, errors.New("private terminal error"), sourcestreamer.ErrFrameTooLarge, previewprocess.ErrCleanupTimeout},
		{"source-containment-viewer-frame", false, sourcestreamer.ErrFrameTooLarge, previewprocess.ErrContainment, nil},
		{"source-joined-frame-timeout", true, nil, errors.Join(sourcestreamer.ErrFrameTooLarge, previewprocess.ErrCleanupTimeout), nil},
		{"forced-viewer-source-timeout", false, previewprocess.ErrForcedCleanup, previewprocess.ErrCleanupTimeout, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, source, view := leaseFixture(t)
			source.stopErr, view.stopErr = tc.source, tc.viewer
			trigger := view
			if tc.terminalOnSource {
				trigger = source
			}
			trigger.waitErr = tc.terminal
			session, err := m.Start(context.Background(), leaseApproval())
			if err != nil {
				t.Fatal(err)
			}
			trigger.retire()
			result := make(chan error, 1)
			go func() { result <- session.Wait() }()
			if err = awaitLeaseResult(t, result); err != ErrCleanupUncertain {
				t.Fatalf("terminal status lost uncertainty: %v", err)
			}
			if source.stops.Load() != 1 || view.stops.Load() != 1 {
				t.Fatal("uncertainty skipped ordered child cleanup")
			}
			assertLeaseRetained(t, m)
		})
	}
}

func TestPreviewLeaseProvedCleanupAllowsFreshRetry(t *testing.T) {
	for _, tc := range []struct {
		name string
		err  error
	}{
		{"clean", nil},
		{"forced-tree-retired", previewprocess.ErrForcedCleanup},
		{"frame-limit", sourcestreamer.ErrFrameTooLarge},
		{"frame-limit-forced-tree-retired", errors.Join(sourcestreamer.ErrFrameTooLarge, previewprocess.ErrForcedCleanup)},
		{"other-safe-failure", errors.New("private child error")},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, source, _ := leaseFixture(t)
			source.stopErr = tc.err
			session, err := m.Start(context.Background(), leaseApproval())
			if err != nil {
				t.Fatal(err)
			}
			err = session.Stop()
			if errors.Is(tc.err, sourcestreamer.ErrFrameTooLarge) {
				if err != ErrFrameTooLarge {
					t.Fatalf("verified frame-limit classification lost: %v", err)
				}
			} else if (err == nil) != (tc.err == nil) || errors.Is(err, ErrCleanupUncertain) {
				t.Fatalf("proved cleanup misclassified: %v", err)
			}
			m.mu.Lock()
			active, blocked := m.active, m.cleanupUncertain
			m.mu.Unlock()
			if active != nil || blocked {
				t.Fatal("proved cleanup retained a reservation")
			}
			m.deps.startStream = func(context.Context, localcomponents.Options, sourcestreamer.Launch) (stream, error) {
				return newLeaseProcess(), nil
			}
			m.deps.startViewer = func(context.Context, string, descriptor) (viewer, error) { return newLeaseProcess(), nil }
			next, err := m.Start(context.Background(), leaseApproval())
			if err != nil {
				t.Fatalf("clean retry blocked: %v", err)
			}
			if next.ID() == session.ID() {
				t.Error("retry reused the prior session")
			}
			if err = next.Stop(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPreviewLeaseStartupProvedCleanupAllowsRetry(t *testing.T) {
	for _, tc := range []struct {
		name, stage           string
		startupErr, sourceErr error
		wantFrame             bool
	}{
		{"source-forced", "source", previewprocess.ErrForcedCleanup, nil, false},
		{"source-canceled", "source", context.Canceled, nil, false},
		{"source-frame", "source", errors.Join(sourcestreamer.ErrFrameTooLarge, previewprocess.ErrForcedCleanup), nil, true},
		{"viewer-forced", "viewer", previewprocess.ErrForcedCleanup, nil, false},
		{"viewer-canceled-source-forced", "viewer", context.Canceled, previewprocess.ErrForcedCleanup, false},
		{"viewer-failed-source-frame", "viewer", errors.New("private renderer error"), errors.Join(sourcestreamer.ErrFrameTooLarge, previewprocess.ErrForcedCleanup), true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			m, source, _ := leaseFixture(t)
			source.stopErr = tc.sourceErr
			if tc.stage == "source" {
				m.deps.startStream = func(context.Context, localcomponents.Options, sourcestreamer.Launch) (stream, error) {
					return nil, tc.startupErr
				}
			} else {
				m.deps.startViewer = func(context.Context, string, descriptor) (viewer, error) { return nil, tc.startupErr }
			}
			if _, err := m.Start(context.Background(), leaseApproval()); err == nil || errors.Is(err, ErrCleanupUncertain) || errors.Is(err, ErrFrameTooLarge) != tc.wantFrame {
				t.Fatalf("proved startup cleanup misclassified: %v", err)
			}
			if tc.stage == "viewer" && source.stops.Load() != 1 {
				t.Fatal("viewer startup failure did not join source")
			}
			if err := m.Stop(); err != nil {
				t.Fatalf("proved cleanup retained a failed reservation: %v", err)
			}
			m.deps.startStream = func(context.Context, localcomponents.Options, sourcestreamer.Launch) (stream, error) {
				return newLeaseProcess(), nil
			}
			m.deps.startViewer = func(context.Context, string, descriptor) (viewer, error) { return newLeaseProcess(), nil }
			next, err := m.Start(context.Background(), leaseApproval())
			if err != nil {
				t.Fatalf("proved startup cleanup blocked fresh retry: %v", err)
			}
			if err = next.Stop(); err != nil {
				t.Fatal(err)
			}
		})
	}
}

func TestPreviewLeaseStartRacingCleanupRechecksBlock(t *testing.T) {
	m, source, _ := leaseFixture(t)
	source.stopErr, source.keepRunning = previewprocess.ErrContainment, true
	if _, err := m.Start(context.Background(), leaseApproval()); err != nil {
		t.Fatal(err)
	}
	entered, release := make(chan struct{}), make(chan struct{})
	// Pause a fresh attempt after its early poison check but before reservation.
	m.deps.permitted = func() bool { close(entered); <-release; return true }
	result := make(chan error, 1)
	go func() { _, err := m.Start(context.Background(), leaseApproval()); result <- err }()
	<-entered
	if err := m.Stop(); err != ErrCleanupUncertain {
		t.Fatalf("cleanup did not poison the manager: %v", err)
	}
	close(release)
	if err := awaitLeaseResult(t, result); err != ErrCleanupUncertain {
		t.Fatalf("racing Start did not recheck cleanup uncertainty: %v", err)
	}
	assertLeaseRetained(t, m)
}

func TestPreviewLeaseStartupCancellationAndDoubleClick(t *testing.T) {
	for _, stage := range []string{"source", "viewer"} {
		for _, cancelWith := range []string{"parent", "manager-stop"} {
			t.Run(stage+"/"+cancelWith, func(t *testing.T) {
				m, source, _ := leaseFixture(t)
				entered, release := make(chan struct{}), make(chan struct{})
				cancelObserved := make(chan struct{})
				var launches atomic.Int32
				waitForCancel := func(ctx context.Context) error {
					launches.Add(1)
					close(entered)
					<-ctx.Done()
					close(cancelObserved)
					<-release
					return errors.Join(ctx.Err(), previewprocess.ErrCleanupTimeout)
				}
				if stage == "source" {
					m.deps.startStream = func(ctx context.Context, _ localcomponents.Options, _ sourcestreamer.Launch) (stream, error) {
						return nil, waitForCancel(ctx)
					}
				} else {
					m.deps.startViewer = func(ctx context.Context, _ string, _ descriptor) (viewer, error) { return nil, waitForCancel(ctx) }
				}
				ctx, cancel := context.WithCancel(context.Background())
				defer cancel()
				startResult := make(chan error, 1)
				go func() { _, err := m.Start(ctx, leaseApproval()); startResult <- err }()
				<-entered
				if _, err := m.Start(context.Background(), leaseApproval()); err == nil {
					t.Fatal("double-click launched during startup")
				}
				stopResult := make(chan error, 1)
				if cancelWith == "parent" {
					cancel()
				} else {
					go func() { stopResult <- m.Stop() }()
				}
				<-cancelObserved
				if _, err := m.Start(context.Background(), leaseApproval()); err == nil {
					t.Fatal("canceled startup released its lease before cleanup")
				}
				close(release)
				if err := awaitLeaseResult(t, startResult); err != ErrCleanupUncertain {
					t.Fatalf("canceled startup lost uncertain cleanup: %v", err)
				}
				if cancelWith == "manager-stop" {
					if err := awaitLeaseResult(t, stopResult); err != ErrCleanupUncertain {
						t.Fatalf("concurrent Stop lost uncertain cleanup: %v", err)
					}
				}
				if launches.Load() != 1 || (stage == "viewer" && source.stops.Load() != 1) {
					t.Fatal("startup overlap or missing source teardown")
				}
				assertLeaseRetained(t, m)
			})
		}
	}
}

func TestPreviewLeaseConcurrentStopCannotReleaseUncertainCleanup(t *testing.T) {
	m, source, view := leaseFixture(t)
	entered, release := make(chan struct{}), make(chan struct{})
	view.stopErr, view.keepRunning = previewprocess.ErrCleanupTimeout, true
	view.onStop = func() { close(entered); <-release }
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	session, err := m.Start(ctx, leaseApproval())
	if err != nil {
		t.Fatal(err)
	}
	cancel()
	<-entered
	const callers = 12
	results := make(chan error, callers)
	for i := 0; i < callers; i++ {
		go func() { results <- m.Stop() }()
	}
	if _, err := m.Start(context.Background(), leaseApproval()); err == nil {
		t.Fatal("fresh launch during teardown")
	}
	close(release)
	for i := 0; i < callers; i++ {
		if err := awaitLeaseResult(t, results); err != ErrCleanupUncertain {
			t.Fatalf("concurrent Stop lost safe status: %v", err)
		}
	}
	if session.Wait() != ErrCleanupUncertain || source.stops.Load() != 1 || view.stops.Load() != 1 {
		t.Fatal("multiple cleanups or incorrect terminal state")
	}
	select {
	case <-view.Done():
		t.Fatal("fixture must retain an unretired child")
	default:
	}
	// Even later observed retirement cannot turn repeated Stop into a reset API.
	view.retire()
	assertLeaseRetained(t, m)
}

func TestPreviewLeaseStatusNeverExposesRawChildErrors(t *testing.T) {
	private := errors.New("private-key-and-child-stderr")
	for _, tc := range []struct {
		name string
		err  error
		want error
	}{
		{"cleanup", errors.Join(private, previewprocess.ErrCleanupTimeout), ErrCleanupUncertain},
		{"containment", fmt.Errorf("wrapped %w", errors.Join(private, previewprocess.ErrContainment)), ErrCleanupUncertain},
		{"frame", errors.Join(private, sourcestreamer.ErrFrameTooLarge), ErrFrameTooLarge},
		{"forced", errors.Join(private, previewprocess.ErrForcedCleanup), nil},
		{"generic", private, nil},
	} {
		t.Run(tc.name, func(t *testing.T) {
			got := previewStatusError("safe failure", tc.err)
			if tc.want != nil && got != tc.want {
				t.Fatalf("safe classification missing: %v", got)
			}
			if tc.want == nil && (got == nil || got.Error() != "safe failure") {
				t.Fatalf("generic safe classification missing: %v", got)
			}
			if strings.Contains(fmt.Sprintf("%s %v %+v %#v", got, got, got, got), private.Error()) || errors.Is(got, private) {
				t.Fatal("private child error escaped the manager")
			}
			for _, raw := range []error{previewprocess.ErrCleanupTimeout, previewprocess.ErrContainment, previewprocess.ErrForcedCleanup, sourcestreamer.ErrFrameTooLarge} {
				if errors.Is(got, raw) {
					t.Fatal("safe status still unwraps to a child error")
				}
			}
		})
	}
	if got := previewStatusError("safe failure", nil, nil); got != nil {
		t.Fatalf("clean completion became an error: %v", got)
	}
}
