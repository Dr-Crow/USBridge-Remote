//go:build windows && cgo

package service

/*
#cgo LDFLAGS: -ld3d11 -ldxgi
int win_hdr_output_active(void);
int d3dx_hevc_main10_supported(void);
*/
import "C"

// HdrDisplaySupported reports whether an HDR10 (HEVC Main10, BT.2020 + PQ)
// stream would actually be shown as HDR right now: Windows HDR is on for the
// monitor the client window is on, and the D3D11VA decoder behind the Vulkan
// renderer takes HEVC Main10 (d3d11_interop_windows.c). Otherwise the client
// asks the host for SDR -- an HDR stream on an SDR display would have to be
// tone-mapped by the D3D11 video processor, which costs ~50% of the 3D
// engine on a Radeon 780M (docs/WINDOWS_DECODE_PIPELINE.md).
func HdrDisplaySupported() bool {
	return C.win_hdr_output_active() != 0 && C.d3dx_hevc_main10_supported() != 0
}
