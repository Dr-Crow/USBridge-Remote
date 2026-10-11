package app

import (
	"context"
	"errors"
	"log"
	"os"
	"sync"
)

var (
	errEngineWorkersSealed    = errors.New("engine workers: registration closed")
	errEngineWorkerDuplicate  = errors.New("engine workers: role already registered")
	errEngineWorkerRetirement = errors.New("engine workers: retirement incomplete")
	errAdminPartialStart      = errors.New("engine workers: partial admin startup")
)

type engineAdminServer interface {
	Serve() error
	Close() error
}

type engineAdminWorker struct {
	ctx       context.Context
	created   chan struct{}
	done      chan struct{}
	closed    chan struct{}
	closeOnce sync.Once
	server    engineAdminServer
	startErr  error
	serveErr  error
	closeErr  error
}

func (w *engineAdminWorker) run() {
	defer close(w.done)
	<-w.created
	if w.server == nil || w.startErr != nil || w.ctx.Err() != nil {
		return
	}
	w.serveErr = cleanHTTPClosed(w.server.Serve())
	if w.serveErr != nil {
		log.Printf("[app] admin socket server error: %v", w.serveErr)
	}
}

func (w *engineAdminWorker) stop() {
	w.closeOnce.Do(func() {
		go func() {
			defer close(w.closed)
			<-w.created
			if w.server != nil {
				w.closeErr = cleanHTTPClosed(w.server.Close())
			}
		}()
	})
}

// This owner covers only the diagnostic signal observer and admin Serve/Close.
// Admin request handlers, authentication callbacks and other engine watchdogs
// are deliberately not claimed joined by this narrow worker receipt.
type engineWorkerOwner struct {
	ctx        context.Context
	cancel     context.CancelFunc
	mu         sync.Mutex
	sealed     bool
	signalDone chan struct{}
	admin      *engineAdminWorker
	closeOnce  sync.Once
	closeErr   error
}

func newEngineWorkerOwner(parent context.Context) *engineWorkerOwner {
	ctx, cancel := context.WithCancel(parent)
	return &engineWorkerOwner{ctx: ctx, cancel: cancel}
}

func (o *engineWorkerOwner) startSignal(notify, unregister func(chan<- os.Signal), report func(os.Signal)) error {
	if notify == nil || unregister == nil || report == nil {
		return errors.New("engine workers: invalid signal observer")
	}
	o.mu.Lock()
	defer o.mu.Unlock()
	if o.sealed || o.ctx.Err() != nil {
		return errEngineWorkersSealed
	}
	if o.signalDone != nil {
		return errEngineWorkerDuplicate
	}
	signals := make(chan os.Signal, 2)
	done := make(chan struct{})
	o.signalDone = done
	notify(signals)
	go func() {
		defer close(done)
		defer unregister(signals)
		select {
		case <-o.ctx.Done():
		case sig := <-signals:
			report(sig)
		}
	}()
	return nil
}

// Reserve ownership before creating the listener. Shutdown racing a slow or
// failed factory still waits for that exact acquisition and its close result.
func (o *engineWorkerOwner) startAdmin(create func() (engineAdminServer, error)) error {
	if create == nil {
		return errors.New("engine workers: invalid admin factory")
	}
	o.mu.Lock()
	if o.sealed || o.ctx.Err() != nil {
		o.mu.Unlock()
		return errEngineWorkersSealed
	}
	if o.admin != nil {
		o.mu.Unlock()
		return errEngineWorkerDuplicate
	}
	w := &engineAdminWorker{ctx: o.ctx, created: make(chan struct{}), done: make(chan struct{}), closed: make(chan struct{})}
	o.admin = w
	o.mu.Unlock()
	go w.run()
	w.server, w.startErr = create()
	if w.server == nil && w.startErr == nil {
		w.startErr = errors.New("engine workers: admin factory returned no server")
	}
	if w.server != nil && w.startErr != nil {
		w.startErr = errors.Join(errAdminPartialStart, w.startErr)
	}
	close(w.created)
	if w.startErr != nil {
		w.stop()
		return w.startErr
	}
	if o.ctx.Err() != nil {
		w.stop()
		return errEngineWorkersSealed
	}
	return nil
}

// The first fixed caller deadline covers signal unregistration, listener close
// and Serve retirement together. Later Close calls retain the first uncertainty.
func (o *engineWorkerOwner) close(ctx context.Context) error {
	o.closeOnce.Do(func() {
		o.mu.Lock()
		o.sealed = true
		signalDone, w := o.signalDone, o.admin
		o.cancel()
		o.mu.Unlock()
		if w != nil {
			w.stop()
		}
		wait := func(done <-chan struct{}) bool {
			if done == nil {
				return true
			}
			select {
			case <-done:
				return true
			case <-ctx.Done():
				o.closeErr = errors.Join(o.closeErr, errEngineWorkerRetirement, ctx.Err())
				return false
			}
		}
		wait(signalDone)
		if w != nil {
			// Read each result only after the channel publishing it is joined.
			if wait(w.created) && errors.Is(w.startErr, errAdminPartialStart) {
				o.closeErr = errors.Join(o.closeErr, w.startErr)
			}
			if wait(w.closed) {
				o.closeErr = errors.Join(o.closeErr, w.closeErr)
			}
			if wait(w.done) {
				o.closeErr = errors.Join(o.closeErr, w.serveErr)
			}
		}
	})
	return o.closeErr
}
