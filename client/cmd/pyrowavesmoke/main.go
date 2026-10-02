// pyrowavesmoke streams from a paired GameStream host with the PyroWave codec,
// without the GUI: it checks that PyroWave is negotiated, counts decoded frames
// for a few seconds, and (USBRIDGE_PYROWAVE_DUMP=<file.pgm>) saves the luma of
// the first decoded frame. No Vulkan window exists here, so frames are decoded
// but not rendered.
//
//	go run -tags usbpass_gousb ./cmd/pyrowavesmoke -host 127.0.0.1 -seconds 10
package main

import (
	"flag"
	"fmt"
	"image"
	"os"
	"sync/atomic"
	"time"

	"github.com/sirupsen/logrus"

	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
)

func main() {
	host := flag.String("host", "127.0.0.1", "GameStream host")
	mode := flag.String("codec", models.VideoModePyroWave, "video mode (pyrowave, h264, h265, av1)")
	width := flag.Int("width", 1920, "stream width")
	height := flag.Int("height", 1080, "stream height")
	fps := flag.Int("fps", 60, "stream fps")
	bitrate := flag.Int("bitrate", 150000, "bitrate, kbps")
	seconds := flag.Int("seconds", 10, "how long to stream")
	flag.Parse()
	logrus.SetLevel(logrus.InfoLevel)

	m := service.NewMoonlightService(models.DefaultConfig())
	m.UpdateHost(*host)
	m.SetVideoMode(*mode)
	m.SetExpectedVideoSize(*width, *height)
	m.SetFPS(*fps)
	m.SetBitrate(*bitrate)
	var frames atomic.Int64
	m.SetOnFrameReceived(func(image.Image) { frames.Add(1) })
	m.SetOnError(func(err error) { logrus.Errorf("stream error: %v", err) })

	if err := m.ConnectToMoonlight(); err != nil {
		fmt.Fprintln(os.Stderr, "connect:", err)
		os.Exit(1)
	}
	start := time.Now()
	deadline := start.Add(time.Duration(*seconds) * time.Second)
	for time.Now().Before(deadline) {
		time.Sleep(time.Second)
		logrus.Infof("frames so far: %d shown, %d PyroWave decoded", frames.Load(), service.PyroWaveDecodedFrames())
	}
	codec, _ := m.NegotiatedVideoCodecName()
	n := frames.Load()
	if codec == models.VideoModePyroWave {
		n = int64(service.PyroWaveDecodedFrames())
	}
	elapsed := time.Since(start).Seconds()
	_ = m.Disconnect()
	fmt.Printf("negotiated=%s frames=%d fps=%.1f\n", codec, n, float64(n)/elapsed)
	if codec != *mode || n == 0 {
		os.Exit(2)
	}
}
