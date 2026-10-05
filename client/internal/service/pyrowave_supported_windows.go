//go:build windows && cgo

package service

// PyroWave decode (pyrowave_decode_windows.c): one static archive from
// scripts/build_pyrowave_windows.sh.

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/pyrowave/vendor/pyrowave
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/pyrowave/build-win -lusbridge-pyrowave -lstdc++
#include <stdint.h>
int pyrowave_win_supported(void);
int pyrowave_win_color_supported(int c444, int hdr);
uint64_t pyrowave_win_frames(void);
*/
import "C"

import "sync"

var (
	pyroWaveSupportedOnce sync.Once
	pyroWaveSupported     bool

	pyroWaveColorMu        sync.Mutex
	pyroWaveColorSupported = map[[2]bool]bool{}
)

// PyroWaveDecodeSupported reports whether this client can decode a PyroWave
// stream (models.VideoModePyroWave). On Windows the decoder runs entirely on
// the GPU, on the renderer's own Vulkan device -- see
// pyrowave_decode_windows.c. Checked once: it needs the shared Vulkan device
// and PyroWave's own feature check to pass.
func PyroWaveDecodeSupported() bool {
	pyroWaveSupportedOnce.Do(func() { pyroWaveSupported = C.pyrowave_win_supported() != 0 })
	return pyroWaveSupported
}

// PyroWaveColorDecodeSupported reports whether this GPU can decode PyroWave
// 4:4:4 (color444) and/or HDR (hdr, 16-bit BT.2020 PQ planes) streams: the
// decode ring's 3-plane format must be storage-writable and sampleable
// (pyrowave_win_color_supported). Checked once per combination.
func PyroWaveColorDecodeSupported(color444, hdr bool) bool {
	if !color444 && !hdr {
		return PyroWaveDecodeSupported()
	}
	key := [2]bool{color444, hdr}
	pyroWaveColorMu.Lock()
	defer pyroWaveColorMu.Unlock()
	if ok, done := pyroWaveColorSupported[key]; done {
		return ok
	}
	c444, chdr := C.int(0), C.int(0)
	if color444 {
		c444 = 1
	}
	if hdr {
		chdr = 1
	}
	ok := C.pyrowave_win_color_supported(c444, chdr) != 0
	pyroWaveColorSupported[key] = ok
	return ok
}

// PyroWaveDecodedFrames counts the PyroWave frames the current stream has
// decoded, rendered or not.
func PyroWaveDecodedFrames() uint64 {
	return uint64(C.pyrowave_win_frames())
}
