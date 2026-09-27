package gui

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"image/png"
	"os"
	"path/filepath"
	"time"

	"github.com/sirupsen/logrus"

	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/service"
)

// Streamer benchmark (gear menu → Run benchmark): for each selected
// streamer the agent switches its backend, the client starts a fresh
// stream and times it to the first frame, then the agent starts the same
// fast-moving test video (agent/internal/benchvideo) from its beginning and
// the client records every frame for a fixed window. Startup is measured
// before the window opens, so the streamers' different start times never
// leak into the comparison; everything inside the window is the same
// content from the same point.

// benchmarkResult is what gets saved as results.json next to chart.png.
type benchmarkResult struct {
	CreatedAt time.Time              `json:"created_at"`
	Duration  float64                `json:"duration_sec"`
	Metrics   []service.BenchMetrics `json:"metrics"`
	Runs      []*service.BenchRun    `json:"runs"`
}

// benchmarkSettleTime lets the new stream get past its first IDR and
// bitrate ramp before the content starts -- the same for every streamer.
const benchmarkSettleTime = time.Second

type benchmarkProgress func(text string, fraction float64)

// runBenchmark performs the whole benchmark; it blocks, so call it from a
// goroutine. The stream and the host's original streamer are restored on
// every exit path.
func (mw *MainWindow) runBenchmark(ctx context.Context, backends []string, window time.Duration, progress benchmarkProgress) (*benchmarkResult, error) {
	client := mw.usbClient
	vw := mw.videoWidget
	if client == nil || vw == nil {
		return nil, errors.New(i18n.Current.ErrorNoConnection)
	}

	progress(i18n.Current.BenchStepStatus, 0)
	status, err := client.BenchStatus()
	if err != nil {
		return nil, err
	}
	original := status.ActiveBackend
	wasStreaming := vw.IsStreaming()

	netGraphWas := service.NetGraphEnabled()
	service.SetNetGraphEnabled(true)

	var recorder service.BenchRecorder
	defer func() {
		recorder.Stop()
		_ = client.BenchVideoStop()
		if !netGraphWas {
			service.SetNetGraphEnabled(false)
		}
		// Put the host back the way the user had it.
		vw.BenchmarkStopStream()
		if original != "" {
			if st, err := client.BenchStatus(); err == nil && st.ActiveBackend != original {
				progress(i18n.Current.BenchStepRestore, 1)
				if _, err := client.BenchSetBackend(original); err != nil {
					logrus.Warnf("📈 [Benchmark] restoring backend %s: %v", original, err)
				}
			}
		}
		if wasStreaming {
			if _, err := vw.BenchmarkStartStream(60 * time.Second); err != nil {
				logrus.Warnf("📈 [Benchmark] restarting the stream: %v", err)
			}
		}
	}()

	progress(i18n.Current.BenchStepPrepare, 0)
	if ready, note, err := client.BenchPrepare(); err != nil {
		return nil, err
	} else if !ready {
		logrus.Warnf("📈 [Benchmark] test video unavailable (%s); the host falls back to a generated pattern", note)
	}

	fps, width, height := vw.BenchmarkStreamConfig()
	result := &benchmarkResult{CreatedAt: time.Now(), Duration: window.Seconds()}
	steps := float64(len(backends))
	for i, kind := range backends {
		if ctx.Err() != nil {
			return nil, ctx.Err()
		}
		label := service.BenchBackendLabel(kind)
		base := float64(i) / steps
		run := &service.BenchRun{Backend: kind, ExpectedFPS: fps, Width: width, Height: height}
		result.Runs = append(result.Runs, run)
		err := mw.benchmarkOne(ctx, run, &recorder, window, func(text string, f float64) {
			progress(fmt.Sprintf(text, label), base+f/steps)
		}, func(text string, left int, f float64) {
			progress(fmt.Sprintf(text, label, left), base+f/steps)
		})
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			run.Error = err.Error()
			logrus.Warnf("📈 [Benchmark] %s: %v", kind, err)
		}
	}
	for _, r := range result.Runs {
		result.Metrics = append(result.Metrics, service.AnalyzeBenchRun(r))
	}
	return result, nil
}

func (mw *MainWindow) benchmarkOne(ctx context.Context, run *service.BenchRun, recorder *service.BenchRecorder, window time.Duration,
	step func(text string, fraction float64), countdown func(text string, left int, fraction float64)) error {
	client, vw := mw.usbClient, mw.videoWidget

	vw.BenchmarkStopStream()
	_ = client.BenchVideoStop()

	step(i18n.Current.BenchStepSwitch, 0.02)
	switchMs, err := client.BenchSetBackend(run.Backend)
	if err != nil {
		return fmt.Errorf("switch: %w", err)
	}
	run.SwitchMs = switchMs

	step(i18n.Current.BenchStepStart, 0.06)
	startup, err := vw.BenchmarkStartStream(90 * time.Second)
	if err != nil {
		return fmt.Errorf("stream: %w", err)
	}
	run.StartupMs = float64(startup.Microseconds()) / 1000
	if codec, ok := vw.NegotiatedVideoCodecName(); ok {
		run.Codec = codec
	}
	logrus.Infof("📈 [Benchmark] %s: switch %.0fms, first frame %.0fms, codec %s", run.Backend, run.SwitchMs, run.StartupMs, run.Codec)

	if err := sleepCtx(ctx, benchmarkSettleTime); err != nil {
		return err
	}
	step(i18n.Current.BenchStepVideo, 0.1)
	content, err := client.BenchVideoStart()
	if err != nil {
		return fmt.Errorf("video: %w", err)
	}
	run.Content = content

	recorder.Start(run)
	started := time.Now()
	ticker := time.NewTicker(250 * time.Millisecond)
	defer ticker.Stop()
	for {
		elapsed := time.Since(started)
		if elapsed >= window {
			break
		}
		countdown(i18n.Current.BenchStepRecord, int((window-elapsed).Seconds()+0.99), 0.1+0.9*elapsed.Seconds()/window.Seconds())
		select {
		case <-ctx.Done():
			recorder.Stop()
			return ctx.Err()
		case <-ticker.C:
		}
	}
	recorder.Stop()
	_ = client.BenchVideoStop()
	vw.BenchmarkStopStream()
	logrus.Infof("📈 [Benchmark] %s: recorded %d frames", run.Backend, len(run.Frames))
	return nil
}

func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

// saveBenchmarkResult writes results.json and chart.png into a new
// timestamped folder and returns it.
func saveBenchmarkResult(res *benchmarkResult) (string, error) {
	base, err := os.UserConfigDir()
	if err != nil {
		base = os.TempDir()
	}
	dir := filepath.Join(base, "usbridge-client", "benchmarks", res.CreatedAt.Format("20060102_150405"))
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	data, err := json.MarshalIndent(res, "", " ")
	if err != nil {
		return "", err
	}
	if err := os.WriteFile(filepath.Join(dir, "results.json"), data, 0o644); err != nil {
		return "", err
	}
	f, err := os.Create(filepath.Join(dir, "chart.png"))
	if err != nil {
		return "", err
	}
	defer f.Close()
	if err := png.Encode(f, service.RenderBenchChart(res.Runs, res.Metrics)); err != nil {
		return "", err
	}
	return dir, nil
}
