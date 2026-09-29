//go:build js && wasm

package webrtcweb

import (
	"sync"
	"syscall/js"
	"testing"
	"time"
)

// fakeEventTarget is a minimal JS object supporting addEventListener/
// removeEventListener/close, shared by the fakePC/fakeDC builders below --
// enough to drive WebRTCClient.Close through the real listener lifecycle
// without a real PeerConnection/DataChannel. close() dispatches "close"
// (and, for the PC fake, "connectionstatechange") asynchronously, like the
// real APIs do, which is exactly the timing the release-ordering bug (see
// Close's own doc comment) depended on.
type fakeEventTarget struct {
	mu        sync.Mutex
	listeners map[string][]js.Value
	closeN    int
	obj       js.Value
}

func newFakeEventTarget(extraCloseEvents ...string) *fakeEventTarget {
	f := &fakeEventTarget{listeners: map[string][]js.Value{}}
	obj := js.Global().Get("Object").New()
	obj.Set("connectionState", "connected")
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
		go func() {
			time.Sleep(5 * time.Millisecond)
			for _, typ := range extraCloseEvents {
				f.dispatch(typ)
			}
		}()
		return nil
	}))
	f.obj = obj
	return f
}

func (f *fakeEventTarget) dispatch(typ string) {
	f.mu.Lock()
	handlers := append([]js.Value(nil), f.listeners[typ]...)
	f.mu.Unlock()
	event := js.Global().Get("Object").New()
	for _, h := range handlers {
		h.Invoke(event)
	}
}

func (f *fakeEventTarget) listenerCount(typ string) int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return len(f.listeners[typ])
}

// newTestWebRTCClient builds a WebRTCClient the same way Connect leaves one
// -- c.pc/c.dc set, all four listener funcs assigned and registered --
// without going through the real offer/answer negotiation Connect performs.
func newTestWebRTCClient(pcFake, dcFake *fakeEventTarget) *WebRTCClient {
	c := &WebRTCClient{}
	pcVal := pcFake.obj
	c.pc = &pcVal
	dcVal := dcFake.obj
	c.dc = &dcVal
	c.videoEl = js.Undefined()
	c.audioEl = js.Undefined()

	c.pcConnStateFunc = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	c.pcTrackFunc = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	c.dcOpenFunc = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })
	c.dcMessageFunc = js.FuncOf(func(_ js.Value, _ []js.Value) any { return nil })

	pcFake.obj.Call("addEventListener", "connectionstatechange", c.pcConnStateFunc)
	pcFake.obj.Call("addEventListener", "track", c.pcTrackFunc)
	dcFake.obj.Call("addEventListener", "open", c.dcOpenFunc)
	dcFake.obj.Call("addEventListener", "message", c.dcMessageFunc)
	return c
}

// TestWebRTCClientClose_DoesNotCrashOnAsyncEvents pins the same class of
// bug fixed in dcConn/wsConn, present at the PeerConnection/DataChannel
// level too: Close() used to call pc.close()/dc.close() and release the
// Go-side js.Func wrappers without ever removing the listeners -- both
// close() calls can still fire "connectionstatechange"/"close" events
// asynchronously afterward, which used to land on already-released funcs.
// A regression here panics the test binary from the async dispatch
// goroutines.
func TestWebRTCClientClose_DoesNotCrashOnAsyncEvents(t *testing.T) {
	pcFake := newFakeEventTarget("connectionstatechange")
	dcFake := newFakeEventTarget("close")
	c := newTestWebRTCClient(pcFake, dcFake)

	c.Close()

	if got := pcFake.listenerCount("connectionstatechange"); got != 0 {
		t.Errorf("pc connectionstatechange listeners after Close = %d, want 0", got)
	}
	if got := pcFake.listenerCount("track"); got != 0 {
		t.Errorf("pc track listeners after Close = %d, want 0", got)
	}
	if got := dcFake.listenerCount("open"); got != 0 {
		t.Errorf("dc open listeners after Close = %d, want 0", got)
	}
	if got := dcFake.listenerCount("message"); got != 0 {
		t.Errorf("dc message listeners after Close = %d, want 0", got)
	}

	// Give the fakes' async event dispatch (5ms) time to actually fire
	// against the now-released funcs.
	time.Sleep(50 * time.Millisecond)
}

// TestWebRTCClientClose_Idempotent pins the closeCalled guard: calling
// Close() twice must not double-release the same js.Funcs (Release()
// panics on a second call).
func TestWebRTCClientClose_Idempotent(t *testing.T) {
	pcFake := newFakeEventTarget()
	dcFake := newFakeEventTarget()
	c := newTestWebRTCClient(pcFake, dcFake)

	c.Close()
	c.Close() // must not panic

	if pcFake.closeN != 1 || dcFake.closeN != 1 {
		t.Errorf("pc/dc close() call counts = %d/%d, want 1/1 (closeCalled should short-circuit the second Close)", pcFake.closeN, dcFake.closeN)
	}
}

// TestWebRTCClientClose_NeverConnected pins Close's nil guards: a
// WebRTCClient whose Connect never ran (or never got as far as setting
// c.pc/c.dc) must not panic by releasing an unset zero-value js.Func.
func TestWebRTCClientClose_NeverConnected(t *testing.T) {
	c := &WebRTCClient{}
	c.Close() // must not panic
}
