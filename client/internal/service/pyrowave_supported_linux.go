//go:build linux && !android && cgo

package service

/*
#include <stdint.h>
extern uint64_t pyrowave_linux_frames(void);
*/
import "C"

// PyroWaveDecodeSupported reports whether this client can decode a PyroWave
// stream (models.VideoModePyroWave). On Linux the decoder is built in:
// Vulkan compute, pyrowave_decode_linux.c.
func PyroWaveDecodeSupported() bool {
	return true
}

// PyroWaveDecodedFrames counts the PyroWave frames the current stream has
// decoded, rendered or not.
func PyroWaveDecodedFrames() uint64 {
	return uint64(C.pyrowave_linux_frames())
}
