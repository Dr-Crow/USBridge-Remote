//go:build js && wasm

package browserapi

import (
	"syscall/js"
	"testing"
)

func withNavigator(t *testing.T, value js.Value) {
	t.Helper()
	// Node's navigator can be a getter. Preserve its exact property descriptor.
	object := js.Global().Get("Object")
	original := object.Call("getOwnPropertyDescriptor", js.Global(), "navigator")
	object.Call("defineProperty", js.Global(), "navigator", map[string]any{"value": value, "configurable": true})
	t.Cleanup(func() {
		if original.IsUndefined() {
			js.Global().Delete("navigator")
		} else {
			object.Call("defineProperty", js.Global(), "navigator", original)
		}
	})
}

func TestOptionalMethodAbsentOrInvalid(t *testing.T) {
	for _, v := range []js.Value{js.Undefined(), js.Null(), js.ValueOf(false), js.ValueOf(7), js.ValueOf("unavailable"), js.ValueOf(map[string]any{}), js.ValueOf(map[string]any{"method": true})} {
		got, ok := Call(v, "method")
		if ok || !got.IsUndefined() {
			t.Fatal("unavailable method was accepted")
		}
	}
}

func TestOptionalMethodPreservesReceiverAndArguments(t *testing.T) {
	receiver := js.ValueOf(map[string]any{"marker": 31})
	calls := 0
	fn := js.FuncOf(func(this js.Value, args []js.Value) any {
		calls++
		if !this.Equal(receiver) || len(args) != 4 || args[0].Float() != 1.5 || args[1].Int() != 2 || args[2].Int() != 3 || args[3].Int() != 4 {
			t.Error("method receiver or arguments changed")
		}
		return 37
	})
	defer fn.Release()
	receiver.Set("method", fn)
	got, ok := Call(receiver, "method", 1.5, 2, 3, 4)
	if !ok || got.Int() != 37 || calls != 1 {
		t.Fatal("method call mismatch")
	}
}

func TestOptionalMethodExceptionIsUnavailable(t *testing.T) {
	// Function constructors are confined to this Node fixture, never shipped
	// in the application or required by its strict browser CSP.
	throwing := js.Global().Get("Function").New("throw new Error('fixture denial');")
	r := js.ValueOf(map[string]any{"method": throwing})
	got, ok := Call(r, "method")
	if ok || !got.IsUndefined() {
		t.Fatal("exception was accepted")
	}
	getter := js.Global().Get("Function").New("throw new Error('fixture getter denial');")
	js.Global().Get("Object").Call("defineProperty", r, "blocked", map[string]any{"get": getter})
	got, ok = Call(r, "blocked")
	if ok || !got.IsUndefined() {
		t.Fatal("throwing getter was accepted")
	}
}

func TestGamepadsUnavailable(t *testing.T) {
	for name, value := range map[string]js.Value{"undefined": js.Undefined(), "null": js.Null(), "absent": js.ValueOf(map[string]any{}), "nonfunction": js.ValueOf(map[string]any{"getGamepads": true})} {
		t.Run(name, func(t *testing.T) {
			withNavigator(t, value)
			if !Gamepads().IsUndefined() {
				t.Fatal("unavailable gamepads accepted")
			}
		})
	}
}

func TestGamepadsReturnShape(t *testing.T) {
	for name, value := range map[string]js.Value{"undefined": js.Undefined(), "null": js.Null(), "object": js.ValueOf(map[string]any{"length": 2}), "boolean": js.ValueOf(false), "number": js.ValueOf(7)} {
		t.Run(name, func(t *testing.T) {
			fn := js.FuncOf(func(js.Value, []js.Value) any { return value })
			defer fn.Release()
			withNavigator(t, js.ValueOf(map[string]any{"getGamepads": fn}))
			if !Gamepads().IsUndefined() {
				t.Fatal("malformed gamepads accepted")
			}
		})
	}
	t.Run("sequence", func(t *testing.T) {
		pads := js.ValueOf([]any{nil, map[string]any{"id": "fixture"}})
		nav := js.ValueOf(map[string]any{})
		fn := js.FuncOf(func(this js.Value, args []js.Value) any {
			if !this.Equal(nav) || len(args) != 0 {
				t.Error("gamepad receiver changed")
			}
			return pads
		})
		defer fn.Release()
		nav.Set("getGamepads", fn)
		withNavigator(t, nav)
		if !Gamepads().Equal(pads) {
			t.Fatal("sequence was changed")
		}
	})
}

func TestGamepadsDeniedOrThrowingNavigator(t *testing.T) {
	throwing := js.Global().Get("Function").New("throw new Error('fixture blocked by browser policy');")
	withNavigator(t, js.ValueOf(map[string]any{"getGamepads": throwing}))
	if !Gamepads().IsUndefined() {
		t.Fatal("denied API accepted")
	}
	js.Global().Get("Object").Call("defineProperty", js.Global(), "navigator", map[string]any{"get": throwing, "configurable": true})
	if !Gamepads().IsUndefined() {
		t.Fatal("throwing navigator accepted")
	}
}
