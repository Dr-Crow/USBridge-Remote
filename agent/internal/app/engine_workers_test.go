package app

import (
	"context"
	"errors"
	"net"
	"net/http"
	"os"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type fakeEngineAdmin struct {
	serving            chan struct{}
	stopped            chan struct{}
	serveErr, closeErr error
	closes             atomic.Int32
	closeOnce          sync.Once
}

func newFakeEngineAdmin() *fakeEngineAdmin {
	return &fakeEngineAdmin{serving: make(chan struct{}), stopped: make(chan struct{})}
}
func (s *fakeEngineAdmin) Serve() error { close(s.serving); <-s.stopped; return s.serveErr }
func (s *fakeEngineAdmin) Close() error {
	s.closes.Add(1)
	s.closeOnce.Do(func() { close(s.stopped) })
	return s.closeErr
}
func closeEngineWorkers(t *testing.T, o *engineWorkerOwner) error {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	return o.close(ctx)
}
func awaitEngineWorker(t *testing.T, ch <-chan struct{}) {
	t.Helper()
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("worker not observed")
	}
}

func TestEngineWorkersSignalNormalAndCancelled(t *testing.T) {
	for _, delivered := range []bool{false, true} {
		t.Run(map[bool]string{false: "cancel", true: "signal"}[delivered], func(t *testing.T) {
			o := newEngineWorkerOwner(context.Background())
			var signals chan<- os.Signal
			var unregistered, reported atomic.Int32
			if err := o.startSignal(func(ch chan<- os.Signal) { signals = ch }, func(ch chan<- os.Signal) {
				if ch != signals {
					t.Error("wrong signal registration")
				}
				unregistered.Add(1)
			}, func(os.Signal) { reported.Add(1) }); err != nil {
				t.Fatal(err)
			}
			if delivered {
				signals <- os.Interrupt
				awaitEngineWorker(t, o.signalDone)
			}
			for i := 0; i < 3; i++ {
				if err := closeEngineWorkers(t, o); err != nil {
					t.Fatal(err)
				}
			}
			if unregistered.Load() != 1 || (reported.Load() == 1) != delivered {
				t.Fatal("signal worker was not joined exactly once")
			}
		})
	}
}
func TestEngineWorkersAdminNormalRepeatedStop(t *testing.T) {
	o := newEngineWorkerOwner(context.Background())
	s := newFakeEngineAdmin()
	if err := o.startAdmin(func() (engineAdminServer, error) { return s, nil }); err != nil {
		t.Fatal(err)
	}
	awaitEngineWorker(t, s.serving)
	var wg sync.WaitGroup
	for i := 0; i < 10; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if err := closeEngineWorkers(t, o); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	if s.closes.Load() != 1 {
		t.Fatal("admin closed more than once")
	}
	awaitEngineWorker(t, o.admin.done)
	awaitEngineWorker(t, o.admin.closed)
	if err := o.startAdmin(func() (engineAdminServer, error) { t.Error("sealed factory executed"); return nil, nil }); err != errEngineWorkersSealed {
		t.Fatal(err)
	}
}
func TestEngineWorkersOptionalFailedStartup(t *testing.T) {
	o := newEngineWorkerOwner(context.Background())
	unavailable := errors.New("listen unavailable")
	if err := o.startAdmin(func() (engineAdminServer, error) { return nil, unavailable }); err != unavailable {
		t.Fatal(err)
	}
	if err := closeEngineWorkers(t, o); err != nil {
		t.Fatal("ordinary optional startup failure changed policy", err)
	}
}
func TestEngineWorkersPartialStartupKeepsErrors(t *testing.T) {
	o := newEngineWorkerOwner(context.Background())
	s := newFakeEngineAdmin()
	failure := errors.New("partial listener failure")
	release := errors.New("listener release failure")
	s.closeErr = errors.Join(net.ErrClosed, release)
	err := o.startAdmin(func() (engineAdminServer, error) { return s, failure })
	if !errors.Is(err, errAdminPartialStart) || !errors.Is(err, failure) {
		t.Fatal(err)
	}
	err = closeEngineWorkers(t, o)
	if !errors.Is(err, errAdminPartialStart) || !errors.Is(err, failure) || !errors.Is(err, release) {
		t.Fatal(err)
	}
	select {
	case <-s.serving:
		t.Fatal("partial startup served")
	default:
	}
	if s.closes.Load() != 1 {
		t.Fatal("partial server not closed")
	}
}
func TestEngineWorkersCloseRacingAcquisition(t *testing.T) {
	o := newEngineWorkerOwner(context.Background())
	s := newFakeEngineAdmin()
	entered, release, result := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		result <- o.startAdmin(func() (engineAdminServer, error) { close(entered); <-release; return s, nil })
	}()
	<-entered
	closed := make(chan error, 1)
	go func() { closed <- closeEngineWorkers(t, o) }()
	awaitEngineWorker(t, o.ctx.Done())
	close(release)
	if err := <-result; err != errEngineWorkersSealed {
		t.Fatal(err)
	}
	if err := <-closed; err != nil {
		t.Fatal(err)
	}
	if s.closes.Load() != 1 {
		t.Fatal("acquired listener leaked")
	}
	select {
	case <-s.serving:
		t.Fatal("cancelled startup served")
	default:
	}
}
func TestEngineWorkersFailedServeAndCloseRetained(t *testing.T) {
	o := newEngineWorkerOwner(context.Background())
	s := newFakeEngineAdmin()
	serveErr, closeErr := errors.New("serve failure"), errors.New("close failure")
	s.serveErr = errors.Join(http.ErrServerClosed, serveErr)
	s.closeErr = closeErr
	if err := o.startAdmin(func() (engineAdminServer, error) { return s, nil }); err != nil {
		t.Fatal(err)
	}
	awaitEngineWorker(t, s.serving)
	err := closeEngineWorkers(t, o)
	if !errors.Is(err, serveErr) || !errors.Is(err, closeErr) {
		t.Fatal(err)
	}
	if again := closeEngineWorkers(t, o); again != err {
		t.Fatal("original result replaced")
	}
}
func TestEngineWorkersTimeoutRemainsUncertain(t *testing.T) {
	o := newEngineWorkerOwner(context.Background())
	entered, release, result := make(chan struct{}), make(chan struct{}), make(chan error, 1)
	go func() {
		result <- o.startAdmin(func() (engineAdminServer, error) {
			close(entered)
			<-release
			return nil, errors.New("late unavailable")
		})
	}()
	<-entered
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := o.close(ctx)
	if !errors.Is(err, errEngineWorkerRetirement) || !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	close(release)
	<-result
	awaitEngineWorker(t, o.admin.done)
	awaitEngineWorker(t, o.admin.closed)
	if again := closeEngineWorkers(t, o); again != err {
		t.Fatal("late join erased uncertainty")
	}
}
func TestEngineWorkersDuplicateAndPrecancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	o := newEngineWorkerOwner(ctx)
	if err := o.startAdmin(func() (engineAdminServer, error) { t.Error("cancelled factory ran"); return nil, nil }); err != errEngineWorkersSealed {
		t.Fatal(err)
	}
	o = newEngineWorkerOwner(context.Background())
	defer closeEngineWorkers(t, o)
	if err := o.startSignal(func(chan<- os.Signal) {}, func(chan<- os.Signal) {}, func(os.Signal) {}); err != nil {
		t.Fatal(err)
	}
	if err := o.startSignal(func(chan<- os.Signal) { t.Error("duplicate registered") }, func(chan<- os.Signal) {}, func(os.Signal) {}); err != errEngineWorkerDuplicate {
		t.Fatal(err)
	}
	s := newFakeEngineAdmin()
	if err := o.startAdmin(func() (engineAdminServer, error) { return s, nil }); err != nil {
		t.Fatal(err)
	}
	if err := o.startAdmin(func() (engineAdminServer, error) { t.Error("duplicate factory ran"); return nil, nil }); err != errEngineWorkerDuplicate {
		t.Fatal(err)
	}
}

func TestEngineWorkersSignalCallbackDeadlineCannotReportJoined(t *testing.T) {
	o := newEngineWorkerOwner(context.Background())
	entered, release := make(chan struct{}), make(chan struct{})
	var signals chan<- os.Signal
	var unregister atomic.Int32
	if err := o.startSignal(func(ch chan<- os.Signal) { signals = ch }, func(chan<- os.Signal) { unregister.Add(1) }, func(os.Signal) { close(entered); <-release }); err != nil {
		t.Fatal(err)
	}
	signals <- os.Interrupt
	awaitEngineWorker(t, entered)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	err := o.close(ctx)
	if !errors.Is(err, errEngineWorkerRetirement) {
		t.Fatal("blocked callback classified joined", err)
	}
	if unregister.Load() != 0 {
		t.Fatal("callback join asserted before completion")
	}
	close(release)
	awaitEngineWorker(t, o.signalDone)
	if unregister.Load() != 1 || closeEngineWorkers(t, o) != err {
		t.Fatal("late callback erased initial uncertainty")
	}
}
