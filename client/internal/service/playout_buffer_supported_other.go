//go:build !(windows && cgo)

package service

// PlayoutBufferSupported: see playout_buffer_supported_windows.go.
func PlayoutBufferSupported() bool {
	return false
}
