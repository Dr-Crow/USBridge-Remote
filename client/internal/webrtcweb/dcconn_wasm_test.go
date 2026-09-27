//go:build js && wasm

package webrtcweb

import (
	"sync"
	"syscall/js"
	"testing"
	"time"
)

// fakeDataChannel is a minimal JS object mimicking the RTCDataChannel
// surface dcConn touches (addEventListener/removeEventListener/close/
// send, plus a "readyState" property) -- enough to drive OpenDataChannel/
// Close through the real event-listener lifecycle without a real
// PeerConnection. Its "close" method dispatches the "close" event
// asynchronously, like a real RTCDataChannel does, which is exactly the
// timing the release-ordering bug (see release's doc comment) depended on.
type fakeDataChannel struct {
	mu        sync.Mutex
	listeners map[string][]js.Value
	closeN    int
	obj       js.Value
}

func newFakeDataChannel() *fakeDataChannel {
	f := &fakeDataChannel{listeners: map[string][]js.Value{}}
	obj := js.Global().Get("Object").New()
	obj.Set("readyState", "connecting")
	obj.Set("binaryType", "")
	obj.Set("addEventListener", js.FuncOf(func(_ js.Value, args []js.Value) any {
		f.mu.Lock()
		defer f.mu.Unlock()
		typ := args[0].String()
		f.listeners[typ] = append(f.listeners[typ], args[1])
		return nil
	}))
	obj.Set("removeEventListener", js.FuncOf(func(_ js.Value, args []js.Value) any {
		f.mu.Lock()
		defer f.mu.Unlock()
		typ := args[0].String()
		handler := args[1]
		kept := f.listeners[typ][:0]
		for _, h := range f.listeners[typ] {
			if !h.Equal(handler) {
				kept = append(kept, h)
			}
		}
		f.listeners[typ] = kept
		return nil
	}))
	obj.Set("close", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		f.mu.Lock()
		f.closeN++
		f.mu.Unlock()
		// Real RTCDataChannel.close() fires "close" as a later task, not
		// synchronously within the call -- reproduce that here, since the
		// bug this guards against only manifests when the event arrives
		// after the caller has already moved on to release().
		go func() {
			time.Sleep(5 * time.Millisecond)
			f.dispatch("close")
		}()
		return nil
	}))
	obj.Set("send", js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil }))
	f.obj = obj
	return f
}

func (f *fakeDataChannel) dispatch(typ string) {
	f.mu.Lock()
	handlers := append([]js.Value(nil), f.listeners[typ]...)
	f.mu.Unlock()
	event := js.Global().Get("Object").New()
	for _, h := range handlers {
		h.Invoke(event)
	}
}

func (f *fakeDataChannel) listenerCount(typ string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.listeners[typ])
}

func newTestDCConn(fake *fakeDataChannel) *dcConn {
	c := &dcConn{
		dc:     fake.obj,
		label:  "api-tunnel",
		recvCh: make(chan []byte, 4),
		done:   make(chan struct{}),
	}
	c.onOpen = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	c.onMsg = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	c.onErr = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	c.onClose = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	fake.obj.Call("addEventListener", "open", c.onOpen)
	fake.obj.Call("addEventListener", "message", c.onMsg)
	fake.obj.Call("addEventListener", "error", c.onErr)
	fake.obj.Call("addEventListener", "close", c.onClose)
	return c
}

// TestDCConnClose_DoesNotCrashOnAsyncCloseEvent pins the actual bug fixed
// live: dcConn.Close() used to call dc.Call("close") and then immediately
// release() the Go-side js.Func wrappers *without removing the JS-side
// listeners first*. A real RTCDataChannel fires its own "close" event
// asynchronously (never synchronously within the close() call), so that
// event routinely arrived after the funcs were already released, crashing
// wasm_exec.js with "call to released function" -- reproduced here on
// every single request through webrtcAPITransport (a fresh DataChannel is
// opened and closed per HTTP call). If release() doesn't detach listeners
// before releasing, this test crashes the whole test binary (Release()
// panics from inside the async dispatch goroutine); if it does, the late
// event finds no listener and this just passes.
func TestDCConnClose_DoesNotCrashOnAsyncCloseEvent(t *testing.T) {
	fake := newFakeDataChannel()
	c := newTestDCConn(fake)

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if got := fake.listenerCount("close"); got != 0 {
		t.Errorf("listenerCount(close) after Close = %d, want 0 -- listener wasn't removed before release", got)
	}

	// Give the fake's async close-event dispatch (5ms) time to actually
	// fire against the now-released funcs. No assertion needed beyond
	// "the process is still alive": a regression here panics the test
	// binary from the dispatch goroutine, which fails this test run.
	time.Sleep(50 * time.Millisecond)
}

// TestDCConnRelease_Idempotent pins releaseOnce: js.Func.Release() panics
// if called a second time on the same Func, and release() is reachable
// from more than one path over a dcConn's lifetime (Close(), and both
// failure branches inside OpenDataChannel) -- nothing before this test
// guaranteed a given dcConn's release() body only ever ran once.
func TestDCConnRelease_Idempotent(t *testing.T) {
	fake := newFakeDataChannel()
	c := newTestDCConn(fake)

	c.release()
	c.release() // must not panic
	c.release() // and again, for good measure

	if got := fake.listenerCount("close"); got != 0 {
		t.Errorf("listenerCount(close) after release = %d, want 0", got)
	}
}

// fakePeerConnection is a minimal JS object standing in for the
// RTCPeerConnection OpenDataChannel calls createDataChannel on.
func fakePeerConnection(dc js.Value) js.Value {
	pc := js.Global().Get("Object").New()
	pc.Set("createDataChannel", js.FuncOf(func(_ js.Value, _ []js.Value) any { return dc }))
	return pc
}

// TestOpenDataChannel_TimeoutClosesTheChannel exercises the real
// WebRTCClient.OpenDataChannel timeout path (readyState never reaches
// "open", no "open" event ever fires) with dcOpenTimeout shrunk for the
// test. Previously that path released the Go-side funcs without ever
// calling dc.close(), leaking an open RTCDataChannel on the shared
// PeerConnection for the rest of the session every time an open attempt
// timed out.
func TestOpenDataChannel_TimeoutClosesTheChannel(t *testing.T) {
	prev := dcOpenTimeout
	dcOpenTimeout = 20 * time.Millisecond
	defer func() { dcOpenTimeout = prev }()

	fake := newFakeDataChannel() // readyState stays "connecting" forever
	pc := fakePeerConnection(fake.obj)
	client := &WebRTCClient{pc: &pc}

	_, err := client.OpenDataChannel("api-tunnel")
	if err == nil {
		t.Fatal("expected a timeout error, got nil")
	}

	if fake.closeN < 1 {
		t.Errorf("dc.close() call count = %d, want at least 1 -- timeout path must close the underlying channel", fake.closeN)
	}
	if got := fake.listenerCount("close"); got != 0 {
		t.Errorf("listenerCount(close) after timeout = %d, want 0 -- release must detach listeners even on the timeout path", got)
	}
}
