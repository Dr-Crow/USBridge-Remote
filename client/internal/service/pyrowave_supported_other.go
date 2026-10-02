//go:build !(linux && !android && cgo)

package service

// PyroWaveDecodeSupported: see pyrowave_supported_linux.go. No PyroWave
// decoder on this platform yet.
func PyroWaveDecodeSupported() bool {
	return false
}

// PyroWaveDecodedFrames: see pyrowave_supported_linux.go.
func PyroWaveDecodedFrames() uint64 {
	return 0
}
