package gui

import (
	"context"
	"fmt"
	"image/color"
	"math"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/service"
)

var benchmarkWindows = []struct {
	label string
	d     time.Duration
}{{"30 s", 30 * time.Second}, {"60 s", time.Minute}, {"2 min", 2 * time.Minute}, {"5 min", 5 * time.Minute}}

// showBenchmarkDialog asks which streamers to compare and for how long.
func (mw *MainWindow) showBenchmarkDialog() {
	if mw.usbClient == nil || mw.videoWidget == nil {
		return
	}
	client := mw.usbClient
	go func() {
		status, err := client.BenchStatus()
		fyne.Do(func() {
			if err != nil {
				dialog.ShowError(fmt.Errorf("%s: %v", i18n.Current.BenchFailed, err), mw.window)
				return
			}
			mw.showBenchmarkSetup(status.AvailableBackends)
		})
	}()
}

func (mw *MainWindow) showBenchmarkSetup(available []string) {
	has := map[string]bool{}
	for _, b := range available {
		has[b] = true
	}
	checks := map[string]*widget.Check{}
	var boxes []fyne.CanvasObject
	for _, kind := range []string{"sunshine", "rustshine"} {
		label := service.BenchBackendLabel(kind)
		if !has[kind] {
			label += " (" + i18n.Current.BenchNotInstalled + ")"
		}
		c := widget.NewCheck(label, nil)
		c.SetChecked(has[kind])
		if !has[kind] {
			c.Disable()
		}
		checks[kind] = c
		boxes = append(boxes, c)
	}
	var labels []string
	for _, w := range benchmarkWindows {
		labels = append(labels, w.label)
	}
	durSel := widget.NewSelect(labels, nil)
	durSel.SetSelected(benchmarkWindows[1].label)

	hint := widget.NewLabel(i18n.Current.BenchHint)
	hint.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(
		container.New(&benchMinWidthLayout{width: 420}, hint),
		container.NewVBox(boxes...),
		container.NewHBox(widget.NewLabel(i18n.Current.BenchDuration), durSel),
	)
	view.ShowCustomConfirmDialog(i18n.Current.BenchTitle, i18n.Current.BenchStart, i18n.Current.Cancel, content, func(ok bool) {
		if !ok {
			return
		}
		var picked []string
		for _, kind := range []string{"sunshine", "rustshine"} {
			if checks[kind].Checked && !checks[kind].Disabled() {
				picked = append(picked, kind)
			}
		}
		if len(picked) == 0 {
			dialog.ShowInformation(i18n.Current.BenchTitle, i18n.Current.BenchNeedOne, mw.window)
			return
		}
		window := benchmarkWindows[1].d
		for _, w := range benchmarkWindows {
			if w.label == durSel.Selected {
				window = w.d
			}
		}
		mw.startBenchmark(picked, window)
	}, mw.window)
}

// benchMinWidthLayout keeps the wrapped hint from collapsing to one word wide.
type benchMinWidthLayout struct{ width float32 }

func (l *benchMinWidthLayout) Layout(objs []fyne.CanvasObject, size fyne.Size) {
	for _, o := range objs {
		o.Resize(size)
		o.Move(fyne.NewPos(0, 0))
	}
}

func (l *benchMinWidthLayout) MinSize(objs []fyne.CanvasObject) fyne.Size {
	h := float32(0)
	for _, o := range objs {
		o.Resize(fyne.NewSize(l.width, o.MinSize().Height))
		h = float32(math.Max(float64(h), float64(o.MinSize().Height)))
	}
	return fyne.NewSize(l.width, h)
}

func (mw *MainWindow) startBenchmark(backends []string, window time.Duration) {
	ctx, cancel := context.WithCancel(context.Background())
	status := widget.NewLabel(i18n.Current.BenchStepStatus)
	bar := widget.NewProgressBar()
	content := container.New(&benchMinWidthLayout{width: 420}, container.NewVBox(status, bar))
	d := dialog.NewCustom(i18n.Current.BenchTitle, i18n.Current.Cancel, content, mw.window)
	d.SetOnClosed(cancel)
	d.Show()

	go func() {
		res, err := mw.runBenchmark(ctx, backends, window, func(text string, f float64) {
			fyne.Do(func() {
				status.SetText(text)
				bar.SetValue(math.Min(math.Max(f, 0), 1))
			})
		})
		cancelled := ctx.Err() != nil
		fyne.Do(func() { d.Hide() })
		cancel()
		if cancelled {
			return
		}
		if err != nil {
			fyne.Do(func() { dialog.ShowError(fmt.Errorf("%s: %v", i18n.Current.BenchFailed, err), mw.window) })
			return
		}
		dir, err := saveBenchmarkResult(res)
		if err != nil {
			logrus.Warnf("📈 [Benchmark] saving results: %v", err)
		}
		for _, m := range res.Metrics {
			logrus.Infof("📈 [Benchmark] %s: startup %.0f+%.0fms, %.1f fps (1%% low %.1f), %d stalls (%d loss/%d host/%d net), host %.1fms p95 %.1fms, net jitter p95 %.1fms, rtt %.1fms, loss %.2f%%, recovery avg %.0fms (idr %d, rfi %d)",
				m.Backend, m.SwitchMs, m.StartupMs, m.AvgFPS, m.Low1FPS, m.StallCount, m.StallLoss, m.StallHost, m.StallNetwork,
				m.HostLatencyAvg, m.HostLatencyP95, m.NetJitterP95, m.RTTAvg, m.PacketLossPct, m.RecoveryAvgMs, m.RecoveredByIDR, m.RecoveredByRFI)
		}
		fyne.Do(func() { mw.showBenchmarkResults(res, dir) })
	}()
}

type benchmarkRow struct {
	section string
	label   string
	value   func(m service.BenchMetrics) (float64, bool)
	format  string
	lower   bool // lower is better
}

func benchmarkRows() []benchmarkRow {
	always := func(f func(service.BenchMetrics) float64) func(service.BenchMetrics) (float64, bool) {
		return func(m service.BenchMetrics) (float64, bool) { return f(m), true }
	}
	host := func(f func(service.BenchMetrics) float64) func(service.BenchMetrics) (float64, bool) {
		return func(m service.BenchMetrics) (float64, bool) { return f(m), m.HostTimingOK }
	}
	return []benchmarkRow{
		{"Startup (measured separately)", "Host streamer switch", always(func(m service.BenchMetrics) float64 { return m.SwitchMs / 1000 }), "%.2f s", true},
		{"", "Stream start → first frame", always(func(m service.BenchMetrics) float64 { return m.StartupMs / 1000 }), "%.2f s", true},
		{"", "Total", always(func(m service.BenchMetrics) float64 { return (m.SwitchMs + m.StartupMs) / 1000 }), "%.2f s", true},

		{"Smoothness (what you see)", "Average fps", always(func(m service.BenchMetrics) float64 { return m.AvgFPS }), "%.1f", false},
		{"", "1% low fps", always(func(m service.BenchMetrics) float64 { return m.Low1FPS }), "%.1f", false},
		{"", "Frame time p50 / p95", always(func(m service.BenchMetrics) float64 { return m.IntervalP95 }), "", true},
		{"", "Frame time p99", always(func(m service.BenchMetrics) float64 { return m.IntervalP99 }), "%.1f ms", true},
		{"", "Worst frame time", always(func(m service.BenchMetrics) float64 { return m.IntervalMax }), "%.0f ms", true},
		{"", "Frame time std-dev", always(func(m service.BenchMetrics) float64 { return m.IntervalStd }), "%.2f ms", true},
		{"", "Stalls", always(func(m service.BenchMetrics) float64 { return float64(m.StallCount) }), "%.0f", true},
		{"", "Time frozen", always(func(m service.BenchMetrics) float64 { return m.StallTotalMs }), "%.0f ms", true},
		{"", "Hitches (>2 frames)", always(func(m service.BenchMetrics) float64 { return float64(m.Hitches) }), "%.0f", true},
		{"", "Client render fps", func(m service.BenchMetrics) (float64, bool) { return m.RenderFPS, m.RenderValid }, "%.1f", false},

		{"Host (capture + encode)", "Encode time avg", always(func(m service.BenchMetrics) float64 { return m.HostLatencyAvg }), "%.2f ms", true},
		{"", "Encode time p95", always(func(m service.BenchMetrics) float64 { return m.HostLatencyP95 }), "%.2f ms", true},
		{"", "Encode time max", always(func(m service.BenchMetrics) float64 { return m.HostLatencyMax }), "%.1f ms", true},
		{"", "Capture rate", host(func(m service.BenchMetrics) float64 { return m.HostFPS }), "%.1f fps", false},
		{"", "Capture pacing std-dev", host(func(m service.BenchMetrics) float64 { return m.HostCadenceStd }), "%.2f ms", true},
		{"", "Longest capture gap", host(func(m service.BenchMetrics) float64 { return m.HostCadenceMax }), "%.0f ms", true},
		{"", "Stalls caused by host", always(func(m service.BenchMetrics) float64 { return float64(m.StallHost) }), "%.0f", true},
		{"", "Bitrate", always(func(m service.BenchMetrics) float64 { return m.BitrateMbps }), "%.1f Mbps", false},
		{"", "Keyframes (IDR)", always(func(m service.BenchMetrics) float64 { return float64(m.IDRFrames) }), "%.0f", true},

		{"Network", "RTT avg", always(func(m service.BenchMetrics) float64 { return m.RTTAvg }), "%.1f ms", true},
		{"", "RTT max", always(func(m service.BenchMetrics) float64 { return m.RTTMax }), "%.1f ms", true},
		{"", "Network jitter avg", host(func(m service.BenchMetrics) float64 { return m.NetJitterAvg }), "%.2f ms", true},
		{"", "Network jitter p95", host(func(m service.BenchMetrics) float64 { return m.NetJitterP95 }), "%.2f ms", true},
		{"", "Frame transfer avg", always(func(m service.BenchMetrics) float64 { return m.TransferAvg }), "%.2f ms", true},
		{"", "Frame transfer p95", always(func(m service.BenchMetrics) float64 { return m.TransferP95 }), "%.2f ms", true},
		{"", "Packet loss (incl. FEC-repaired)", always(func(m service.BenchMetrics) float64 { return m.PacketLossPct }), "%.3f %%", true},
		{"", "FEC failed", always(func(m service.BenchMetrics) float64 { return float64(m.FecFailed) }), "%.0f", true},
		{"", "Stalls caused by network", always(func(m service.BenchMetrics) float64 { return float64(m.StallNetwork) }), "%.0f", true},

		{"Recovery from lost frames", "Loss events", always(func(m service.BenchMetrics) float64 { return float64(m.LossEvents) }), "%.0f", true},
		{"", "Frames lost", always(func(m service.BenchMetrics) float64 { return float64(m.LostFrames) }), "%.0f", true},
		{"", "Recovery time avg", func(m service.BenchMetrics) (float64, bool) { return m.RecoveryAvgMs, m.LossEvents > 0 }, "%.0f ms", true},
		{"", "Recovery time max", func(m service.BenchMetrics) (float64, bool) { return m.RecoveryMaxMs, m.LossEvents > 0 }, "%.0f ms", true},
		{"", "Recovered by IDR / RFI", always(func(m service.BenchMetrics) float64 { return float64(m.RecoveredByIDR) }), "", true},
	}
}

func (mw *MainWindow) showBenchmarkResults(res *benchmarkResult, dir string) {
	metrics := res.Metrics
	cols := 1 + len(metrics)
	var cells []fyne.CanvasObject
	text := func(s string, c color.Color, bold bool) fyne.CanvasObject {
		t := canvas.NewText(s, c)
		t.TextSize = 13
		t.TextStyle.Bold = bold
		return t
	}
	cells = append(cells, text("", design.ColorTextLight, true))
	for _, m := range metrics {
		cells = append(cells, text(service.BenchBackendLabel(m.Backend), service.BenchBackendColor(m.Backend), true))
	}
	best := color.NRGBA{R: 0x4a, G: 0xd6, B: 0x6d, A: 0xff}
	for _, row := range benchmarkRows() {
		if row.section != "" {
			cells = append(cells, text(row.section, design.ColorTextLight, true))
			for range metrics {
				cells = append(cells, text("", design.ColorTextLight, false))
			}
		}
		cells = append(cells, text("   "+row.label, design.ColorTextMuted, false))
		vals := make([]float64, len(metrics))
		oks := make([]bool, len(metrics))
		for i, m := range metrics {
			vals[i], oks[i] = row.value(m)
			if m.Error != "" {
				oks[i] = false
			}
		}
		bestIdx := benchmarkBest(vals, oks, row.lower)
		for i, m := range metrics {
			s := "n/a"
			switch {
			case m.Error != "":
				s = "failed"
			case !oks[i]:
			case row.label == "Frame time p50 / p95":
				s = fmt.Sprintf("%.1f / %.1f ms", m.IntervalP50, m.IntervalP95)
			case row.label == "Recovered by IDR / RFI":
				s = fmt.Sprintf("%d / %d", m.RecoveredByIDR, m.RecoveredByRFI)
			default:
				s = fmt.Sprintf(row.format, vals[i])
			}
			c := color.Color(design.ColorTextLight)
			if i == bestIdx {
				c = best
			}
			cells = append(cells, text(s, c, i == bestIdx))
		}
	}
	table := container.NewGridWithColumns(cols, cells...)

	var errs []string
	for _, m := range metrics {
		if m.Error != "" {
			errs = append(errs, fmt.Sprintf("%s: %s", service.BenchBackendLabel(m.Backend), m.Error))
		}
	}

	img := service.RenderBenchChart(res.Runs, metrics)
	chart := canvas.NewImageFromImage(img)
	chart.FillMode = canvas.ImageFillContain
	w := float32(900)
	chart.SetMinSize(fyne.NewSize(w, w*float32(img.Bounds().Dy())/float32(img.Bounds().Dx())))

	stalls := container.NewVBox(text(i18n.Current.BenchStalls, design.ColorTextLight, true))
	for _, m := range metrics {
		stalls.Add(text(service.BenchBackendLabel(m.Backend), service.BenchBackendColor(m.Backend), true))
		if len(m.Stalls) == 0 {
			stalls.Add(text("   "+i18n.Current.BenchNoStalls, design.ColorTextMuted, false))
		}
		for _, st := range m.Stalls {
			line := fmt.Sprintf("   %7.2fs  %5.0f ms  %s", st.AtMs/1000, st.DurationMs, st.Cause)
			if st.LostFrames > 0 {
				line += fmt.Sprintf(" (%d frames lost, recovered by %s)", st.LostFrames, strings.ToUpper(st.Recovery))
			}
			if st.Cause == service.BenchCauseHost {
				line += fmt.Sprintf(" (host captured nothing for %.0f ms)", st.HostGapMs)
			}
			stalls.Add(text(line, service.BenchCauseColor(st.Cause), false))
		}
	}

	header := container.NewVBox()
	if dir != "" {
		header.Add(widget.NewLabel(fmt.Sprintf(i18n.Current.BenchSavedTo, dir)))
	}
	for _, e := range errs {
		header.Add(text(e, service.BenchCauseColor(service.BenchCauseLoss), false))
	}
	if len(res.Runs) > 0 && res.Runs[0].Content != "" {
		header.Add(text(fmt.Sprintf("Content: %s, %d fps configured, %.0f s per streamer", res.Runs[0].Content, res.Runs[0].ExpectedFPS, res.Duration), design.ColorTextMuted, false))
	}

	scroll := container.NewVScroll(container.NewVBox(header, table, widget.NewSeparator(), chart, widget.NewSeparator(), stalls))
	d := dialog.NewCustom(i18n.Current.BenchResultsTitle, i18n.Current.Close, scroll, mw.window)
	sz := mw.window.Canvas().Size()
	d.Resize(fyne.NewSize(sz.Width*0.94, sz.Height*0.94))
	d.Show()
}

// benchmarkBest returns the index of the best value, or -1 when there is
// nothing to compare or all compared values are equal.
func benchmarkBest(vals []float64, oks []bool, lower bool) int {
	best, n := -1, 0
	for i, v := range vals {
		if !oks[i] {
			continue
		}
		n++
		if best < 0 || (lower && v < vals[best]) || (!lower && v > vals[best]) {
			best = i
		}
	}
	if n < 2 {
		return -1
	}
	for i, v := range vals {
		if oks[i] && i != best && v == vals[best] {
			return -1
		}
	}
	return best
}

// benchmarkMenuAction is the gear menu's "Run benchmark" handler, nil
// where the build can't record frames (web).
func (mw *MainWindow) benchmarkMenuAction() func() {
	if !service.BenchSupported() {
		return nil
	}
	return mw.showBenchmarkDialog
}
