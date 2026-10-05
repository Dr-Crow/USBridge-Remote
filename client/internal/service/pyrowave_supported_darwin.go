//go:build darwin && !ios && cgo

package service

/*
#include <stdint.h>
extern uint64_t pyrowave_darwin_frames(void);
*/
import "C"

// PyroWaveDecodeSupported reports whether this client can decode a PyroWave
// stream (models.VideoModePyroWave). On macOS the decoder is built in:
// Vulkan compute over MoltenVK, pyrowave_decode_darwin.m.
func PyroWaveDecodeSupported() bool {
	return true
}

// PyroWaveDecodedFrames counts the PyroWave frames the current stream has
// decoded, rendered or not.
func PyroWaveDecodedFrames() uint64 {
	return uint64(C.pyrowave_darwin_frames())
}
