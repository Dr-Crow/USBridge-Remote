//go:build !(windows && cgo) && !(darwin && !ios && cgo) && !(ios && cgo) && !(linux && !android && cgo) && !(android && cgo)

package service

// PlayoutBufferSupported: see playout_buffer_supported_windows.go,
// playout_buffer_supported_darwin.go, playout_buffer_supported_ios.go,
// playout_buffer_supported_linux.go and playout_buffer_supported_android.go.
// What's left here is platforms without a native moonlight-common-c decode
// path at all (e.g. the wasm/WebRTC web client, which has its own
// browser-native jitter handling).
func PlayoutBufferSupported() bool {
	return false
}
