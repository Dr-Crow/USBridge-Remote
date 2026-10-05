//go:build darwin && !ios && cgo

package main

/*
#include <stdint.h>
*/
import "C"

// goMetalMouseEvent: see cmd/metalspike/main.go's identical stub doc comment --
// this package doesn't import internal/gui/controller (which normally provides
// it), so metal_video_impl_darwin.m's mouse handlers need their own stub to
// satisfy the linker. pyrowavesmoke never opens a window, so these never fire.
//
//export goMetalMouseEvent
func goMetalMouseEvent(typ C.int, x, y C.float, btn C.int) {}
