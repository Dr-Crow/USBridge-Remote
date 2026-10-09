//go:build linux && !android && cgo

package platform

/*
#cgo pkg-config: libavcodec libavutil libswscale
#include <stdlib.h>
#include "uplink_camera_linux.h"
*/
import "C"

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"sync/atomic"
	"unsafe"

	"github.com/sirupsen/logrus"
)

// cameraBitrate: plenty for a 720p30 webcam picture, little next to the
// stream itself.
const cameraBitrate = 2_500_000

// ownCameraName is the virtual camera a USBridge agent on this same machine
// would add for a stream's camera: sending it back would be a loop.
const ownCameraName = "USBridge Camera"

// CameraSupported reports whether StartCameraCapture can work in this build.
func CameraSupported() bool { return true }

// ListCameras returns the V4L2 capture devices, by node.
func ListCameras() []CameraInfo {
	nodes, _ := filepath.Glob("/sys/class/video4linux/video*")
	sort.Slice(nodes, func(i, j int) bool { return videoIndex(nodes[i]) < videoIndex(nodes[j]) })
	var out []CameraInfo
	for _, n := range nodes {
		name, _ := os.ReadFile(filepath.Join(n, "name"))
		label := strings.TrimSpace(string(name))
		if strings.HasPrefix(label, ownCameraName) {
			continue
		}
		dev := "/dev/" + filepath.Base(n)
		cdev := C.CString(dev)
		ok := C.cam_is_capture_device(cdev) != 0
		C.free(unsafe.Pointer(cdev))
		if !ok {
			continue
		}
		if label == "" {
			label = dev
		}
		out = append(out, CameraInfo{ID: dev, Name: label})
	}
	return out
}

func videoIndex(path string) int {
	var n int
	fmt.Sscanf(strings.TrimPrefix(filepath.Base(path), "video"), "%d", &n)
	return n
}

type cameraCapture struct {
	stop atomic.Bool
	done chan struct{}
	once sync.Once
}

func (c *cameraCapture) Stop() {
	c.once.Do(func() {
		c.stop.Store(true)
		<-c.done
	})
}

// StartCameraCapture captures the camera id (a ListCameras ID), encodes it
// to H.264 and hands every access unit to onFrame from its own goroutine;
// onFrame returns false when the access unit couldn't go out, and the next
// one is then a keyframe.
func StartCameraCapture(id string, onFrame func(au []byte, keyframe bool) bool) (UplinkCapture, error) {
	var errBuf [256]C.char
	cpath := C.CString(id)
	cam := C.cam_open(cpath, C.int(cameraBitrate), &errBuf[0], C.int(len(errBuf)))
	C.free(unsafe.Pointer(cpath))
	if cam == nil {
		return nil, fmt.Errorf("%s", C.GoString(&errBuf[0]))
	}
	var w, h, ew, eh C.int
	C.cam_size(cam, &w, &h, &ew, &eh)
	logrus.Infof("📷 [UPLINK] camera %s: %dx%d, encoding %dx%d with %s", id, w, h, ew, eh, C.GoString(C.cam_encoder_name(cam)))

	c := &cameraCapture{done: make(chan struct{})}
	go func() {
		defer close(c.done)
		defer C.cam_close(cam)
		forceKey := true
		for !c.stop.Load() {
			var out *C.uint8_t
			var key C.int
			force := C.int(0)
			if forceKey {
				force = 1
			}
			n := C.cam_read_encode(cam, force, &out, &key)
			if n < 0 {
				logrus.Warnf("📷 [UPLINK] camera %s stopped delivering pictures", id)
				return
			}
			if n == 0 {
				continue
			}
			au := C.GoBytes(unsafe.Pointer(out), n)
			forceKey = false
			if !onFrame(au, key != 0) {
				forceKey = true
			}
		}
	}()
	return c, nil
}
