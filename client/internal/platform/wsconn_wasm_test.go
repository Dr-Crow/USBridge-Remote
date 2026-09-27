//go:build js && wasm

package platform

import (
	"errors"
	"sync"
	"syscall/js"
	"testing"
	"time"
)

// fakeWebSocket is a minimal JS object mimicking the browser WebSocket
// surface wsConn touches (onopen/onmessage/onerror/onclose property
// assignment, close(), send()) -- enough to drive DialWebSocket/Close
// through the real handler lifecycle without a real network connection.
// Its "close" method dispatches onclose asynchronously, like a real
// WebSocket does, which is exactly the timing the release-ordering bug
// (see release's doc comment) depended on.
type fakeWebSocket struct {
	mu     sync.Mutex
	closeN int
	obj    js.Value
}

func newFakeWebSocket() *fakeWebSocket {
	f := &fakeWebSocket{}
	obj := js.Global().Get("Object").New()
	obj.Set("onopen", js.Null())
	obj.Set("onmessage", js.Null())
	obj.Set("onerror", js.Null())
	obj.Set("onclose", js.Null())
	obj.Set("close", js.FuncOf(func(_ js.Value, _ []js.Value) any {
		f.mu.Lock()
		f.closeN++
		f.mu.Unlock()
		// Real WebSocket.close() fires "onclose" as a later task, not
		// synchronously within the call -- reproduce that here, since the
		// bug this guards against only manifests when the event arrives
		// after the caller has already moved on to release().
		go func() {
			time.Sleep(5 * time.Millisecond)
			handler := obj.Get("onclose")
			if handler.Type() == js.TypeFunction {
				handler.Invoke(js.Global().Get("Object").New())
			}
		}()
		return nil
	}))
	obj.Set("send", js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil }))
	f.obj = obj
	return f
}

func (f *fakeWebSocket) handlerAttached(name string) bool {
	return f.obj.Get(name).Type() == js.TypeFunction
}

func newTestWSConn(fake *fakeWebSocket) *wsConn {
	c := &wsConn{
		ws:     fake.obj,
		url:    "wss://test/",
		recvCh: make(chan []byte, 4),
		done:   make(chan struct{}),
	}
	c.onOpen = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	c.onMsg = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	c.onErr = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	c.onClose = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	fake.obj.Set("onopen", c.onOpen)
	fake.obj.Set("onmessage", c.onMsg)
	fake.obj.Set("onerror", c.onErr)
	fake.obj.Set("onclose", c.onClose)
	return c
}

// TestWSConnClose_DoesNotCrashOnAsyncCloseEvent pins the same bug fixed in
// webrtcweb/dcconn_wasm.go's dcConn, present here too (this type was
// structurally copied from/to that one -- see this file's own doc
// comment): Close() used to call ws.Call("close") and then immediately
// release() the Go-side js.Func wrappers without detaching the WebSocket's
// onclose/onmessage/etc. handlers first. A real WebSocket fires "close"
// asynchronously, so that event routinely arrived after the funcs were
// already released, crashing wasm_exec.js with "call to released
// function". A regression here panics the test binary from the async
// dispatch goroutine.
func TestWSConnClose_DoesNotCrashOnAsyncCloseEvent(t *testing.T) {
	fake := newFakeWebSocket()
	c := newTestWSConn(fake)

	if err := c.Close(); err != nil {
		t.Fatalf("Close: %v", err)
	}
	if fake.handlerAttached("onclose") {
		t.Error("onclose handler still attached after Close -- wasn't detached before release")
	}

	// Give the fake's async close-event dispatch (5ms) time to actually
	// fire against the now-released funcs.
	time.Sleep(50 * time.Millisecond)
}

// TestWSConnRelease_Idempotent pins releaseOnce: js.Func.Release() panics
// if called a second time on the same Func, and release() is reachable
// from more than one path over a wsConn's lifetime (Close(), and both
// failure branches inside DialWebSocket).
func TestWSConnRelease_Idempotent(t *testing.T) {
	fake := newFakeWebSocket()
	c := newTestWSConn(fake)

	c.release()
	c.release() // must not panic
	c.release()

	if fake.handlerAttached("onclose") {
		t.Error("onclose handler still attached after release")
	}
}

// TestWSConnTimeoutCleanup_ClosesTheSocket pins the cleanup sequence
// DialWebSocket's timeout branch runs (fail, ws.Call("close"), release) --
// DialWebSocket itself has no injection point for a fake WebSocket
// constructor (it calls js.Global().Get("WebSocket").New(url) directly),
// so this drives that same sequence directly against a wsConn built the
// same way DialWebSocket builds one, rather than waiting out a real dial.
// Previously this branch released the Go-side funcs without ever calling
// ws.close(), leaking an open WebSocket for the rest of the session every
// time a dial timed out.
func TestWSConnTimeoutCleanup_ClosesTheSocket(t *testing.T) {
	fake := newFakeWebSocket() // no onopen ever fires
	c := newTestWSConn(fake)

	c.fail(errors.New("websocket: connect timeout"))
	c.ws.Call("close")
	c.release()

	if fake.closeN < 1 {
		t.Errorf("ws.close() call count = %d, want at least 1", fake.closeN)
	}
	if fake.handlerAttached("onclose") {
		t.Error("onclose handler still attached after timeout release")
	}
}
