package app

import (
	"testing"

	"usbridge_agent/internal/config"
	"usbridge_agent/internal/streamhost"
)

// fakeCaptureBackend is just enough of a streamhost.Backend for the
// benchmark's monitor pin: its device list and its own output pick.
type fakeCaptureBackend struct {
	streamhost.Backend
	devices []streamhost.CaptureDevice
	output  string
	sets    int
	running bool
}

func (f *fakeCaptureBackend) ListCaptureDevices() []streamhost.CaptureDevice { return f.devices }
func (f *fakeCaptureBackend) OutputName() string                             { return f.output }
func (f *fakeCaptureBackend) Running() bool                                  { return f.running }
func (f *fakeCaptureBackend) SetOutputName(name string) error {
	f.output = name
	f.sets++
	return nil
}

const (
	laptopPanel  = `\\.\DISPLAY1`
	monitorAbove = `\\.\DISPLAY6`
	panelGUID    = "{26932b0f-6861-553f-b009-2caec1fc240f}"
	aboveGUID    = "{8eddf53f-c324-5dc2-bc77-88163bc237b2}"
)

// The same two monitors as each streamer names them: rustshine by DXGI
// index, Sunshine by its device_id GUID.
func rustshineDevices() []streamhost.CaptureDevice {
	return []streamhost.CaptureDevice{{OutputName: "0", GDIName: laptopPanel}, {OutputName: "1", GDIName: monitorAbove}}
}

func sunshineDevices() []streamhost.CaptureDevice {
	return []streamhost.CaptureDevice{{OutputName: panelGUID, GDIName: laptopPanel}, {OutputName: aboveGUID, GDIName: monitorAbove}}
}

func TestBenchMonitorPinsEachStreamerToTheSameMonitor(t *testing.T) {
	a := &App{benchMonitor: monitorAbove}
	rust := &fakeCaptureBackend{devices: rustshineDevices(), output: "0"}
	sun := &fakeCaptureBackend{devices: sunshineDevices(), output: panelGUID}

	if changed, ok := a.applyBenchMonitor(rust, "rustshine"); !ok || !changed || rust.output != "1" {
		t.Fatalf("rustshine: changed=%v ok=%v output=%q, want monitor_index 1", changed, ok, rust.output)
	}
	if changed, ok := a.applyBenchMonitor(sun, "sunshine"); !ok || !changed || sun.output != aboveGUID {
		t.Fatalf("sunshine: changed=%v ok=%v output=%q, want %s", changed, ok, sun.output, aboveGUID)
	}
	if a.benchOrigOutput["rustshine"] != "0" || a.benchOrigOutput["sunshine"] != panelGUID {
		t.Fatalf("originals %v, want each streamer's own pick", a.benchOrigOutput)
	}

	// Applying again (the benchmark switches back and forth) changes
	// nothing and keeps the real originals, not the benchmark's value.
	if changed, ok := a.applyBenchMonitor(rust, "rustshine"); !ok || changed || rust.sets != 1 {
		t.Fatalf("second apply: changed=%v ok=%v sets=%d", changed, ok, rust.sets)
	}
	if a.benchOrigOutput["rustshine"] != "0" {
		t.Fatalf("original overwritten with %q", a.benchOrigOutput["rustshine"])
	}
}

func TestBenchMonitorUnknownToTheStreamerIsLeftAlone(t *testing.T) {
	a := &App{benchMonitor: `\\.\DISPLAY9`}
	rust := &fakeCaptureBackend{devices: rustshineDevices(), output: "0"}
	if changed, ok := a.applyBenchMonitor(rust, "rustshine"); ok || changed || rust.sets != 0 {
		t.Fatalf("changed=%v ok=%v sets=%d, want nothing touched", changed, ok, rust.sets)
	}
	// Sunshine that hasn't run yet reports no devices at all.
	sun := &fakeCaptureBackend{output: ""}
	a.benchMonitor = monitorAbove
	if _, ok := a.applyBenchMonitor(sun, "sunshine"); ok {
		t.Fatal("pinned a Sunshine that reports no monitors")
	}
}

func TestBenchMonitorWithoutPinDoesNothing(t *testing.T) {
	a := &App{}
	rust := &fakeCaptureBackend{devices: rustshineDevices(), output: "0"}
	if changed, ok := a.applyBenchMonitor(rust, "rustshine"); !ok || changed || rust.sets != 0 {
		t.Fatalf("changed=%v ok=%v sets=%d", changed, ok, rust.sets)
	}
}

// Releasing the pin puts back the active streamer's pick in place and the
// inactive one's in its config file.
func TestBenchMonitorReleaseRestoresBothStreamers(t *testing.T) {
	state := t.TempDir()
	a := &App{cfg: config.Config{StateDir: state}, exeDir: t.TempDir(), benchMonitor: monitorAbove}
	rust := &fakeCaptureBackend{devices: rustshineDevices(), output: "0"}
	a.stream, a.streamKind = rust, "rustshine"

	// The benchmark ran Sunshine too: its config now names the monitor above.
	sunshineConf := streamhost.NewSunshine(a.exeDir, state, "")
	if err := sunshineConf.SetOutputName(panelGUID); err != nil {
		t.Fatal(err)
	}
	sun := &fakeCaptureBackend{devices: sunshineDevices(), output: panelGUID}
	a.applyBenchMonitor(sun, "sunshine")
	if err := sunshineConf.SetOutputName(aboveGUID); err != nil {
		t.Fatal(err)
	}
	a.applyBenchMonitor(rust, "rustshine")

	a.benchMonitor = ""
	if err := a.restoreBenchOutputs(false); err != nil {
		t.Fatal(err)
	}
	if rust.output != "0" {
		t.Errorf("rustshine output %q after release, want its own 0", rust.output)
	}
	if got := streamhost.NewSunshine(a.exeDir, state, "").OutputName(); got != panelGUID {
		t.Errorf("sunshine.conf output_name %q after release, want %s", got, panelGUID)
	}
	if a.benchOrigOutput != nil {
		t.Errorf("originals kept after release: %v", a.benchOrigOutput)
	}
}

// Releasing with deferRestart leaves the running streamer alone and marks
// its restart for the benchmark's next backend call; the monitor pick is
// still put back in place right away.
func TestBenchMonitorReleaseCanDeferTheActiveRestart(t *testing.T) {
	a := &App{cfg: config.Config{StateDir: t.TempDir()}, exeDir: t.TempDir(), benchMonitor: monitorAbove}
	// Running, and with no real Stop/Start behind it: an immediate restart
	// would panic on the embedded nil Backend.
	rust := &fakeCaptureBackend{devices: rustshineDevices(), output: "0", running: true}
	a.stream, a.streamKind = rust, "rustshine"
	a.applyBenchMonitor(rust, "rustshine")

	a.benchMonitor = ""
	if err := a.restoreBenchOutputs(true); err != nil {
		t.Fatal(err)
	}
	if rust.output != "0" {
		t.Errorf("output %q after release, want its own 0", rust.output)
	}
	if !a.benchRestartPending {
		t.Error("restart of the running streamer not marked as pending")
	}
}
