//go:build !linux || android || !cgo

package platform

import "fmt"

// CameraSupported: camera capture is only implemented on Linux so far.
func CameraSupported() bool { return false }

// ListCameras: see CameraSupported.
func ListCameras() []CameraInfo { return nil }

// StartCameraCapture: see CameraSupported.
func StartCameraCapture(id string, onFrame func(au []byte, keyframe bool) bool) (UplinkCapture, error) {
	return nil, fmt.Errorf("camera capture is not supported on this platform")
}
