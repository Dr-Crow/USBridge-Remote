//go:build windows && cgo

package service

// PyroWave decode (pyrowave_decode_windows.c): one static archive from
// scripts/build_pyrowave_windows.sh.

/*
#cgo CFLAGS: -I${SRCDIR}/../../third_party/pyrowave/vendor/pyrowave
#cgo LDFLAGS: -L${SRCDIR}/../../third_party/pyrowave/build-win -lusbridge-pyrowave -lstdc++
#include <stdint.h>
int pyrowave_win_supported(void);
uint64_t pyrowave_win_frames(void);
*/
import "C"

import "sync"

var (
	pyroWaveSupportedOnce sync.Once
	pyroWaveSupported     bool
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

// PyroWaveDecodedFrames counts the PyroWave frames the current stream has
// decoded, rendered or not.
func PyroWaveDecodedFrames() uint64 {
	return uint64(C.pyrowave_win_frames())
}
