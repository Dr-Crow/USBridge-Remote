package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"sync"
	"time"
)

// usbPassBridgePreambleMaxLen bounds the one-line preamble StartUSBPassBridge
// reads off each new connection before treating it as an opaque relay --
// generous enough for "GAMEPAD browser-pen-1234567890123\n" with room to
// spare, tight enough that a misbehaving/garbled dialer can't make this read
// forever.
const usbPassBridgePreambleMaxLen = 256

const usbBridgeConnectionLimit = 64

// USBPassBridge owns the listener and every connection admitted through it.
// Close interrupts I/O immediately; success means Accept and all handlers joined.
// A timed-out Close does not claim retirement. It can be called again to observe
// final retirement, without restarting the listener or any worker.
type USBPassBridge struct {
	listener    net.Listener
	ctx         context.Context
	cancel      context.CancelFunc
	stopOnce    sync.Once
	mu          sync.Mutex
	stopping    bool
	connections map[net.Conn]struct{}
	closeErr    error
	handlers    sync.WaitGroup
	accepted    chan struct{}
	done        chan struct{}
}

func startUSBPassBridgeOwner(parent context.Context, listen func() (net.Listener, error), handle func(context.Context, net.Conn)) (*USBPassBridge, error) {
	if parent == nil || listen == nil || handle == nil {
		return nil, errors.New("usbpass bridge: missing owner")
	}
	if err := parent.Err(); err != nil {
		return nil, err
	}
	listener, err := listen()
	if err != nil {
		if listener != nil {
			// A custom/future listener returned partial ownership. Never classify
			// its cleanup, even apparently successful, as simple unavailability.
			return nil, errors.Join(errors.New("usbpass bridge: partial listener startup"), err, listener.Close())
		}
		return nil, usbBridgeUnavailableError{cause: err}
	}
	if listener == nil {
		return nil, errors.New("usbpass bridge: missing listener")
	}
	ctx, cancel := context.WithCancel(parent)
	b := &USBPassBridge{listener: listener, ctx: ctx, cancel: cancel, connections: make(map[net.Conn]struct{}), accepted: make(chan struct{}), done: make(chan struct{})}
	if err := ctx.Err(); err != nil {
		cancel()
		closeErr := listener.Close()
		if errors.Is(closeErr, net.ErrClosed) {
			closeErr = nil
		}
		return nil, errors.Join(err, closeErr)
	}
	go b.accept(handle)
	go func() {
		select {
		case <-ctx.Done():
		case <-b.accepted:
		}
		b.stop()
		<-b.accepted // No handler can be added after this barrier.
		b.handlers.Wait()
		close(b.done)
	}()
	return b, nil
}

func (b *USBPassBridge) Addr() string { return b.listener.Addr().String() }

func (b *USBPassBridge) recordClose(err error) {
	if err != nil && !errors.Is(err, net.ErrClosed) {
		b.closeErr = errors.Join(b.closeErr, err)
	}
}

func (b *USBPassBridge) accept(handle func(context.Context, net.Conn)) {
	defer close(b.accepted)
	for {
		conn, err := b.listener.Accept()
		if err != nil {
			b.mu.Lock()
			if !b.stopping && b.ctx.Err() == nil {
				b.recordClose(err)
			}
			b.mu.Unlock()
			return
		}
		b.mu.Lock()
		if b.stopping || b.ctx.Err() != nil || len(b.connections) >= usbBridgeConnectionLimit {
			b.recordClose(conn.Close())
			b.mu.Unlock()
			continue
		}
		b.connections[conn] = struct{}{}
		b.handlers.Add(1)
		b.mu.Unlock()
		go func() {
			defer b.handlers.Done()
			defer func() {
				b.mu.Lock()
				b.recordClose(conn.Close())
				delete(b.connections, conn)
				b.mu.Unlock()
			}()
			handle(b.ctx, conn)
		}()
	}
}

func (b *USBPassBridge) stop() {
	b.stopOnce.Do(func() {
		b.cancel()
		b.mu.Lock()
		defer b.mu.Unlock()
		b.stopping = true
		b.recordClose(b.listener.Close())
		for conn := range b.connections {
			b.recordClose(conn.Close())
		}
	})
}

func (b *USBPassBridge) Close(ctx context.Context) error {
	if ctx == nil {
		return errors.New("usbpass bridge: missing close deadline")
	}
	b.stop()
	joined := func() error { b.mu.Lock(); defer b.mu.Unlock(); return b.closeErr }
	select {
	case <-b.done:
		return joined()
	default:
	}
	select {
	case <-b.done:
		return joined()
	case <-ctx.Done():
		return errors.Join(errors.New("usbpass bridge: retirement incomplete"), ctx.Err())
	}
}

// relayUSBPassBridge owns both copies. Either direction ending closes both
// sockets and joins the other direction before returning to the bridge owner.
func relayUSBPassBridge(ctx context.Context, conn net.Conn, reader *bufio.Reader, address string) error {
	target, err := (&net.Dialer{Timeout: 5 * time.Second}).DialContext(ctx, "tcp4", address)
	if err != nil {
		return err
	}
	return copyUSBPassBridge(conn, reader, target)
}

func copyUSBPassBridge(conn net.Conn, reader *bufio.Reader, target net.Conn) error {
	done := make(chan error, 2)
	go func() { _, err := io.Copy(target, reader); done <- err }()
	go func() { _, err := io.Copy(conn, target); done <- err }()
	first := <-done
	closeConn, closeTarget := conn.Close(), target.Close()
	second := <-done
	// Closing the opposite direction is the intended retirement mechanism.
	clean := func(err error) error {
		if errors.Is(err, net.ErrClosed) {
			return nil
		}
		return err
	}
	return errors.Join(clean(first), clean(second), clean(closeConn), clean(closeTarget))
}

func readUSBPassBridgePreamble(reader *bufio.Reader) (string, error) {
	line, err := reader.ReadSlice('\n')
	if err != nil {
		return "", err
	}
	return string(line), nil
}

func (b *USBPassBridge) State() string {
	b.mu.Lock()
	defer b.mu.Unlock()
	select {
	case <-b.done:
		if b.closeErr != nil {
			return "cleanup_failed"
		}
		return "closed"
	default:
	}
	if b.stopping || b.ctx.Err() != nil {
		return "closing"
	}
	select {
	case <-b.accepted:
		return "closing"
	default:
	}
	return "ready"
}

// Only a failure that created no listener is eligible for the caller's explicit
// degraded-start policy. Joined/partial failures deliberately lack this marker.
type usbBridgeUnavailableError struct{ cause error }

func (e usbBridgeUnavailableError) Error() string              { return "usbpass bridge: listener unavailable" }
func (e usbBridgeUnavailableError) Unwrap() error              { return e.cause }
func (e usbBridgeUnavailableError) USBBridgeUnavailable() bool { return true }
