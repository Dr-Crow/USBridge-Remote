//go:build js && wasm

// Package browserapi guards optional browser APIs without requesting access,
// changing permissions, or substituting another transport.
package browserapi

import "syscall/js"

// Call invokes an optional method with its original receiver. Missing APIs and
// JavaScript exceptions (for example a blocked Gamepad API) are unavailable.
// Recovery is confined to this browser boundary; callers' Go work is unaffected.
func Call(receiver js.Value, name string, args ...any) (value js.Value, ok bool) {
	value = js.Undefined()
	defer func() {
		if recover() != nil {
			value, ok = js.Undefined(), false
		}
	}()
	if receiver.Type() != js.TypeObject && receiver.Type() != js.TypeFunction {
		return value, false
	}
	if js.Global().Get("Reflect").Call("get", receiver, name).Type() != js.TypeFunction {
		return value, false
	}
	return receiver.Call(name, args...), true
}

// Gamepads returns the browser's gamepad sequence, or undefined if absent,
// denied, or malformed. It does not prompt or open a device.
func Gamepads() (pads js.Value) {
	pads = js.Undefined()
	defer func() {
		if recover() != nil {
			pads = js.Undefined()
		}
	}()
	value, ok := Call(js.Global().Get("Reflect").Call("get", js.Global(), "navigator"), "getGamepads")
	if !ok || value.IsNull() || value.IsUndefined() || !js.Global().Get("Array").Call("isArray", value).Bool() {
		return pads
	}
	return value
}
