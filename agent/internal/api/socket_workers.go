package api

import (
	"context"
	"errors"
	"io"
	"net"
	"sync"

	"github.com/gorilla/websocket"
)

// socketWorkers owns only the route's transport workers. Close must unblock all
// socket I/O before joining; the enclosing HTTP owner retains its fixed shutdown
// deadline if a handler or transport violates that contract. This does not claim
// ownership of clipboard callbacks or browser session exporters.
type socketWorkers struct {
	ctx         context.Context
	cancel      context.CancelFunc
	closeOnce   sync.Once
	closers     []func() error
	closeErr    error
	watcherDone chan struct{}
	wg          sync.WaitGroup
}

func newSocketWorkers(parent context.Context, closers ...func() error) *socketWorkers {
	ctx, cancel := context.WithCancel(parent)
	w := &socketWorkers{ctx: ctx, cancel: cancel, closers: closers, watcherDone: make(chan struct{})}
	// A dial may hand back an owned socket just as its context expires. Do not
	// leave such a socket open until a new watcher happens to be scheduled.
	if ctx.Err() != nil {
		w.closeIO()
	}
	go func() {
		defer close(w.watcherDone)
		<-ctx.Done()
		w.closeIO()
	}()
	return w
}

// Worker registration is completed by the route before closeAndJoin is called.
// A worker may request closeIO, but must never wait for its own join.
func (w *socketWorkers) goWorker(fn func()) {
	w.wg.Add(1)
	go func() { defer w.wg.Done(); fn() }()
}

func cleanSocketClosed(err error) error {
	original := err
	for depth := 0; err != nil && depth < 64; depth++ {
		if err == net.ErrClosed {
			return nil
		}
		if _, joined := err.(interface{ Unwrap() []error }); joined {
			return original
		}
		err = errors.Unwrap(err)
	}
	return original
}

func (w *socketWorkers) closeIO() {
	w.closeOnce.Do(func() {
		w.cancel()
		for _, closeSocket := range w.closers {
			w.closeErr = errors.Join(w.closeErr, cleanSocketClosed(closeSocket()))
		}
	})
}

func (w *socketWorkers) closeAndJoin() error {
	w.closeIO()
	<-w.watcherDone
	w.wg.Wait()
	return w.closeErr
}

// The legacy byte protocol is unchanged. Either relay ending or request
// cancellation closes both transports and joins both directions before return.
func relayWebsocketBroker(ctx context.Context, conn *websocket.Conn, broker net.Conn) error {
	w := newSocketWorkers(ctx, conn.Close, broker.Close)
	results := make(chan error, 2)
	w.goWorker(func() {
		defer w.closeIO()
		results <- websocketBrokerOutput(conn, broker)
	})
	w.goWorker(func() {
		defer w.closeIO()
		results <- websocketBrokerInput(conn, broker)
	})
	<-w.ctx.Done()
	closeErr := w.closeAndJoin()
	return errors.Join(cleanRelayEnd(<-results), cleanRelayEnd(<-results), closeErr)
}

func websocketBrokerOutput(conn *websocket.Conn, broker net.Conn) error {
	buf := make([]byte, 64*1024)
	for {
		n, err := broker.Read(buf)
		if n > 0 {
			if writeErr := conn.WriteMessage(websocket.BinaryMessage, buf[:n]); writeErr != nil {
				return errors.Join(err, writeErr)
			}
		}
		if err != nil {
			return err
		}
	}
}

func websocketBrokerInput(conn *websocket.Conn, broker net.Conn) error {
	for {
		_, data, err := conn.ReadMessage()
		if err != nil {
			return err
		}
		n, err := broker.Write(data)
		if err != nil {
			return err
		}
		if n != len(data) {
			return io.ErrShortWrite
		}
	}
}

func cleanRelayEnd(err error) error {
	if err == io.EOF || err == io.ErrClosedPipe || websocket.IsCloseError(err, websocket.CloseNormalClosure, websocket.CloseGoingAway) {
		return nil
	}
	return cleanSocketClosed(err)
}
