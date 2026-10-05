//go:build !(linux && !android && cgo) && !(darwin && !ios && cgo) && !(windows && cgo)

package service

// PyroWaveDecodeSupported: see pyrowave_supported_linux.go,
// pyrowave_supported_darwin.go and pyrowave_supported_windows.go. No PyroWave
// decoder on this platform yet.
func PyroWaveDecodeSupported() bool {
	return false
}

// PyroWaveDecodedFrames: see pyrowave_supported_linux.go.
func PyroWaveDecodedFrames() uint64 {
	return 0
}
