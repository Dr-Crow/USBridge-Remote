//go:build darwin && !ios && cgo

package service

// Embeds the hand-ported FSR1 (EASU+RCAS) + bilinear-resample MSL source
// (see fsr/fsr_metal.metal's own doc comment for why this is compiled at
// RUNTIME via newLibraryWithSource rather than at build time) and hands it
// to metal_video_impl_darwin.m once, before any frame render, so the first
// real frame never pays the shader-compile stall the spike measured (~636ms
// -- see this package's git history / the plan this came from for that
// finding). init() runs at process start, well before any stream connects.

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#include <stdint.h>
#include <stdlib.h>
extern void metal_fsr_set_shader_source(const char *src);
*/
import "C"

import (
	_ "embed"
	"unsafe"
)

//go:embed fsr/fsr_metal.metal
var fsrMetalSource string

func init() {
	cs := C.CString(fsrMetalSource)
	defer C.free(unsafe.Pointer(cs))
	C.metal_fsr_set_shader_source(cs)
}
