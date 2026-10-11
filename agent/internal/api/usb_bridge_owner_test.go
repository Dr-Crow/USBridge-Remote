package api

import (
	"bufio"
	"context"
	"errors"
	"io"
	"net"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

func bridgeListen() (net.Listener, error) { return net.Listen("tcp4", "127.0.0.1:0") }
func closeBridge(t *testing.T, b *USBPassBridge) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := b.Close(ctx); err != nil {
		t.Fatal(err)
	}
	select {
	case <-b.done:
	default:
		t.Fatal("close did not join")
	}
}

func TestUSBBridgeOwnerStartupFailures(t *testing.T) {
	denied := errors.New("listen failed")
	if b, err := startUSBPassBridgeOwner(context.Background(), func() (net.Listener, error) { return nil, denied }, func(context.Context, net.Conn) {}); b != nil || !errors.Is(err, denied) {
		t.Fatal("listen failure lost")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if b, err := startUSBPassBridgeOwner(ctx, func() (net.Listener, error) { t.Fatal("listened after cancellation"); return nil, nil }, func(context.Context, net.Conn) {}); b != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("pre-cancel failed")
	}
	var listener net.Listener
	ctx, cancel = context.WithCancel(context.Background())
	b, err := startUSBPassBridgeOwner(ctx, func() (net.Listener, error) { var e error; listener, e = bridgeListen(); cancel(); return listener, e }, func(context.Context, net.Conn) { t.Fatal("handler after failed startup") })
	if b != nil || !errors.Is(err, context.Canceled) {
		t.Fatal("canceled handoff accepted")
	}
	if _, err = net.DialTimeout("tcp4", listener.Addr().String(), 20*time.Millisecond); err == nil {
		t.Fatal("failed-start listener retained")
	}
}

func TestUSBBridgeOwnerClosesPreambleAndRepeatedClose(t *testing.T) {
	entered, returned := make(chan struct{}), make(chan struct{})
	b, err := startUSBPassBridgeOwner(context.Background(), bridgeListen, func(_ context.Context, c net.Conn) {
		close(entered)
		_, _ = bufio.NewReader(c).ReadString('\n')
		close(returned)
	})
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp4", b.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("handler absent")
	}
	closeBridge(t, b)
	closeBridge(t, b)
	select {
	case <-returned:
	default:
		t.Fatal("handler not joined")
	}
	if c, err := net.DialTimeout("tcp4", b.Addr(), 20*time.Millisecond); err == nil {
		c.Close()
		t.Fatal("listener survived close")
	}
}

func TestUSBBridgeOwnerConcurrentAcceptAndClose(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	var active atomic.Int32
	b, err := startUSBPassBridgeOwner(ctx, bridgeListen, func(_ context.Context, c net.Conn) {
		active.Add(1)
		defer active.Add(-1)
		_, _ = io.Copy(io.Discard, c)
	})
	if err != nil {
		t.Fatal(err)
	}
	var wg sync.WaitGroup
	for range 24 {
		wg.Go(func() {
			c, err := net.DialTimeout("tcp4", b.Addr(), time.Second)
			if err == nil {
				defer c.Close()
				_, _ = c.Write([]byte("partial"))
			}
		})
	}
	cancel()
	closeBridge(t, b)
	wg.Wait()
	closeBridge(t, b)
	if active.Load() != 0 {
		t.Fatal("handler escaped owner")
	}
}

func TestUSBBridgeOwnerIncompleteJoinIsFailure(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})
	b, err := startUSBPassBridgeOwner(context.Background(), bridgeListen, func(context.Context, net.Conn) { close(entered); <-release })
	if err != nil {
		t.Fatal(err)
	}
	c, err := net.Dial("tcp4", b.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer c.Close()
	<-entered
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	if err := b.Close(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatal("unjoined handler counted as closed")
	}
	select {
	case <-b.done:
		t.Fatal("false retirement")
	default:
	}
	close(release)
	closeBridge(t, b)
}

func TestUSBBridgeRelayJoinsBothDirections(t *testing.T) {
	for _, closeSource := range []bool{true, false} {
		source, client := net.Pipe()
		target, server := net.Pipe()
		result := make(chan error, 1)
		go func() { result <- copyUSBPassBridge(source, bufio.NewReader(source), target) }()
		sent := make(chan struct{})
		go func() { defer close(sent); _, _ = client.Write([]byte("request")) }()
		buf := make([]byte, 7)
		if _, err := io.ReadFull(server, buf); err != nil || string(buf) != "request" {
			t.Fatal("forward bytes changed")
		}
		<-sent
		go func() { _, _ = server.Write([]byte("reply")) }()
		buf = make([]byte, 5)
		if _, err := io.ReadFull(client, buf); err != nil || string(buf) != "reply" {
			t.Fatal("reverse bytes changed")
		}
		if closeSource {
			client.Close()
		} else {
			server.Close()
		}
		select {
		case <-result:
		case <-time.After(time.Second):
			t.Fatal("copy directions did not join")
		}
		client.Close()
		server.Close()
	}
}

func TestUSBBridgeRelayCanceledDial(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	local, peer := net.Pipe()
	defer local.Close()
	defer peer.Close()
	if err := relayUSBPassBridge(ctx, local, bufio.NewReader(local), "127.0.0.1:1"); !errors.Is(err, context.Canceled) {
		t.Fatal("canceled dial lost")
	}
}

func TestUSBBridgePreambleBound(t *testing.T) {
	for _, n := range []int{0, 1, 254, 255, 256, 4096} {
		input := strings.Repeat("x", n) + "\n"
		line, err := readUSBPassBridgePreamble(bufio.NewReaderSize(strings.NewReader(input), usbPassBridgePreambleMaxLen))
		if n < 256 && (err != nil || line != input) {
			t.Fatal("bounded preamble changed")
		}
		if n >= 256 && (err == nil || line != "") {
			t.Fatal("oversized preamble admitted")
		}
	}
}

func TestUSBBridgeOwnerAdmissionBound(t *testing.T) {
	entered := make(chan struct{}, usbBridgeConnectionLimit+1)
	b, err := startUSBPassBridgeOwner(context.Background(), bridgeListen, func(_ context.Context, c net.Conn) { entered <- struct{}{}; _, _ = io.Copy(io.Discard, c) })
	if err != nil {
		t.Fatal(err)
	}
	var connections []net.Conn
	defer func() {
		for _, c := range connections {
			c.Close()
		}
		closeBridge(t, b)
	}()
	for range usbBridgeConnectionLimit {
		c, err := net.Dial("tcp4", b.Addr())
		if err != nil {
			t.Fatal(err)
		}
		connections = append(connections, c)
		select {
		case <-entered:
		case <-time.After(time.Second):
			t.Fatal("admitted handler missing")
		}
	}
	extra, err := net.Dial("tcp4", b.Addr())
	if err != nil {
		t.Fatal(err)
	}
	defer extra.Close()
	extra.SetReadDeadline(time.Now().Add(time.Second))
	var one [1]byte
	if _, err := extra.Read(one[:]); err != io.EOF {
		t.Fatal("saturated connection not retired")
	}
	select {
	case <-entered:
		t.Fatal("saturated handler started")
	default:
	}
	closeBridge(t, b)
}

type errorCloseListener struct {
	net.Listener
	failure error
	closes  atomic.Int32
}

func (l *errorCloseListener) Close() error { l.closes.Add(1); _ = l.Listener.Close(); return l.failure }
func TestUSBBridgeOwnerCloseErrorCannotPass(t *testing.T) {
	listener, err := bridgeListen()
	if err != nil {
		t.Fatal(err)
	}
	sentinel := errors.New("listener release uncertain")
	l := &errorCloseListener{Listener: listener, failure: sentinel}
	b, err := startUSBPassBridgeOwner(context.Background(), func() (net.Listener, error) { return l, nil }, func(context.Context, net.Conn) {})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	for range 2 {
		if err = b.Close(ctx); !errors.Is(err, sentinel) {
			t.Fatal("release uncertainty lost")
		}
	}
	if b.State() != "cleanup_failed" {
		t.Fatal("close uncertainty advertised ready")
	}
	if l.closes.Load() != 1 {
		t.Fatal("listener closed repeatedly")
	}
	select {
	case <-b.done:
	default:
		t.Fatal("failure resources not joined")
	}
}

func TestUSBBridgePartialListenerNeverOrdinaryUnavailable(t *testing.T) {
	for _, closeErr := range []error{nil, errors.New("close uncertain")} {
		listener, err := bridgeListen()
		if err != nil {
			t.Fatal(err)
		}
		l := &errorCloseListener{Listener: listener, failure: closeErr}
		startup := errors.New("partial start")
		b, err := startUSBPassBridgeOwner(context.Background(), func() (net.Listener, error) { return l, startup }, func(context.Context, net.Conn) { t.Fatal("partial startup admitted handler") })
		if b != nil || !errors.Is(err, startup) || (closeErr != nil && !errors.Is(err, closeErr)) || l.closes.Load() != 1 {
			t.Fatal("partial startup cleanup lost")
		}
		if _, ok := err.(interface{ USBBridgeUnavailable() bool }); ok {
			t.Fatal("partial startup degraded")
		}
	}
}
