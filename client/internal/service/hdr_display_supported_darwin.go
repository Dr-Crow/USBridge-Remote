//go:build darwin && !ios && cgo

package service

// HdrDisplaySupported reports whether this client can actually show an HDR
// (HEVC Main10, BT.2020 + PQ) stream as HDR. macOS only: VideoToolbox decodes
// to the 10-bit x420 IOSurface and Core Animation composites it with EDR
// (metal_video_impl_darwin.m, metal_video_set_hdr). Every other client would
// decode Main10 through an 8-bit path with no PQ handling -- a washed-out
// picture -- so the video dialog doesn't offer HDR there.
func HdrDisplaySupported() bool {
	return true
}
