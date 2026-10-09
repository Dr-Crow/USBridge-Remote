//go:build linux && !android && cgo

package platform

import (
	"encoding/binary"
	"os"
	"testing"
	"time"
)

// USBRIDGE_CAMERA_LIVE=/dev/videoN go test -run TestCameraCaptureLive; with
// USBRIDGE_CAMERA_OUT the stream is saved as Annex B, with
// USBRIDGE_CAMERA_AUS as access units (LE32 length, keyframe byte, data) --
// what the streamer's camera test replays.
func TestCameraCaptureLive(t *testing.T) {
	dev := os.Getenv("USBRIDGE_CAMERA_LIVE")
	if dev == "" {
		t.Skip("USBRIDGE_CAMERA_LIVE not set")
	}
	t.Logf("cameras: %+v", ListCameras())
	var stream, aus []byte
	secs := 3 * time.Second
	if os.Getenv("USBRIDGE_CAMERA_AUS") != "" {
		secs = 10 * time.Second
	}
	var frames, keys int
	c, err := StartCameraCapture(dev, func(au []byte, key bool) bool {
		stream = append(stream, au...)
		aus = binary.LittleEndian.AppendUint32(aus, uint32(len(au)))
		if key {
			aus = append(aus, 1)
		} else {
			aus = append(aus, 0)
		}
		aus = append(aus, au...)
		frames++
		if key {
			keys++
		}
		return true
	})
	if err != nil {
		t.Fatal(err)
	}
	time.Sleep(secs)
	c.Stop()
	t.Logf("%d access units (%d keyframes), %d bytes = %.2f Mbit/s", frames, keys, len(stream), float64(len(stream))*8/secs.Seconds()/1e6)
	if frames < 60 || keys < 1 {
		t.Fatalf("too few frames: %d, keyframes %d", frames, keys)
	}
	if out := os.Getenv("USBRIDGE_CAMERA_OUT"); out != "" {
		_ = os.WriteFile(out, stream, 0o644)
	}
	if out := os.Getenv("USBRIDGE_CAMERA_AUS"); out != "" {
		_ = os.WriteFile(out, aus, 0o644)
	}
}
