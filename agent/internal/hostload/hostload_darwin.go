//go:build darwin

package hostload

import (
	"bufio"
	"bytes"
	"log"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
)

// macOS: CPU from ps (%cpu per process: the machine is their sum, the
// streamer its own; ps's %cpu is a short decaying average, close enough
// for a benchmark's half-second ticks), GPU from the IOAccelerator's
// PerformanceStatistics in ioreg ("Device Utilization %" as 3d -- Apple
// GPUs, and AMD/Intel ones in Intel Macs, report it without root). The
// streamer's GPU share isn't available per process.

func logf(format string, args ...any) { log.Printf("[hostload] "+format, args...) }

type reader struct{}

func newReader() (*reader, error) {
	if _, err := exec.LookPath("ps"); err != nil {
		return nil, err
	}
	return &reader{}, nil
}

func (*reader) close() {}

func (*reader) read() (Sample, error) {
	out, err := exec.Command("ps", "-A", "-o", "%cpu=,comm=").Output()
	if err != nil {
		return Sample{}, err
	}
	var s Sample
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) < 2 {
			continue
		}
		v, err := strconv.ParseFloat(f[0], 64)
		if err != nil {
			continue
		}
		s.CPU += v
		if isStreamer(filepath.Base(strings.Join(f[1:], " "))) {
			s.StreamerCPU += v
		}
	}
	// ps: 100% is one core.
	n := float64(runtime.NumCPU())
	s.CPU /= n
	s.StreamerCPU /= n
	if s.CPU > 100 {
		s.CPU = 100
	}
	if gpu, ok := ioregGPU(); ok {
		s.GPU = map[string]float64{"3d": gpu}
	}
	return s, nil
}

var gpuUtilRe = regexp.MustCompile(`"Device Utilization %"\s*=\s*(\d+)`)

// ioregGPU is the busiest GPU's "Device Utilization %".
func ioregGPU() (float64, bool) {
	out, err := exec.Command("ioreg", "-r", "-d", "1", "-c", "IOAccelerator").Output()
	if err != nil {
		return 0, false
	}
	var max float64
	found := false
	for _, m := range gpuUtilRe.FindAllSubmatch(out, -1) {
		if v, err := strconv.ParseFloat(string(m[1]), 64); err == nil {
			found = true
			if v > max {
				max = v
			}
		}
	}
	return max, found
}
