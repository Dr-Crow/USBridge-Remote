package gui

import (
	"archive/zip"
	"context"
	"encoding/json"
	"fmt"
	"image/color"
	"image/png"
	"io"
	"math"
	"strings"
	"sync/atomic"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"

	"usbridge-client/internal/api"
	"usbridge-client/internal/gui/design"
	"usbridge-client/internal/gui/i18n"
	"usbridge-client/internal/gui/view"
	"usbridge-client/internal/models"
	"usbridge-client/internal/service"
)

var benchmarkWindows = []struct {
	label string
	d     time.Duration
}{{"30 s", 30 * time.Second}, {"60 s", time.Minute}, {"2 min", 2 * time.Minute}, {"5 min", 5 * time.Minute}}

// benchmarkBusy is set from opening the setup dialog until the benchmark
// ends (or the dialog is cancelled), so a repeated menu click can't stack a
// second dialog or start a second run on top of the first.
var benchmarkBusy atomic.Bool

// benchmarkRunFn and saveBenchmarkResultFn are variables only so tests can
// drive the dialog flow without a host.
var (
	benchmarkRunFn        = (*MainWindow).runBenchmark
	saveBenchmarkResultFn = saveBenchmarkResult
)

// showBenchmarkDialog asks which streamers to compare and for how long.
func (mw *MainWindow) showBenchmarkDialog() {
	if mw.usbClient == nil || mw.videoWidget == nil {
		return
	}
	if !benchmarkBusy.CompareAndSwap(false, true) {
		return
	}
	client := mw.usbClient
	go func() {
		status, err := client.BenchStatus()
		fyne.Do(func() {
			if err != nil {
				benchmarkBusy.Store(false)
				view.ShowErrorDialog(fmt.Errorf("%s: %v", i18n.Current.BenchFailed, err), mw.window)
				return
			}
			mw.showBenchmarkSetup(status.AvailableBackends, status.Monitors)
		})
	}()
}

// benchMonitorChoice is one entry of the setup dialog's monitor list.
type benchMonitorChoice struct {
	label string
	id    string
}

// benchMonitorChoices labels the host's monitors for the setup dialog and
// picks the default: the primary monitor, else the first listed.
func benchMonitorChoices(mons []api.BenchMonitor) (choices []benchMonitorChoice, def int) {
	for i, m := range mons {
		name := m.Name
		short := strings.TrimPrefix(m.ID, `\\.\`)
		if name == "" {
			name = short
		} else if short != m.ID {
			name += " (" + short + ")"
		}
		label := fmt.Sprintf("%s · %dx%d", name, m.Width, m.Height)
		if m.Primary {
			label += " · " + i18n.Current.BenchMonitorPrimary
			def = i
		}
		choices = append(choices, benchMonitorChoice{label: label, id: m.ID})
	}
	return choices, def
}

func (mw *MainWindow) showBenchmarkSetup(available []string, mons []api.BenchMonitor) {
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
	durSel := view.NewHeaderDropdown(labels, benchmarkWindows[1].label, nil)

	// Both streamers capture, and the test video plays on, the one monitor
	// picked here -- otherwise each streamer used its own saved monitor and
	// the video landed wherever the player opened. Hidden when the host
	// can't list its monitors (then nothing is pinned).
	choices, defChoice := benchMonitorChoices(mons)
	var monSel *view.HeaderDropdown
	settings := []fyne.CanvasObject{container.NewVBox(boxes...), widget.NewSeparator()}
	if len(choices) > 0 {
		var monLabels []string
		for _, c := range choices {
			monLabels = append(monLabels, c.label)
		}
		monSel = view.NewHeaderDropdown(monLabels, choices[defChoice].label, nil)
		settings = append(settings, container.NewBorder(nil, nil, benchText(i18n.Current.BenchMonitor, design.ColorTextMuted, 13, false), nil, monSel))
	}
	codecSel := view.NewHeaderDropdown(benchCodecLabels(), benchCodecs[0].label(), nil)
	settings = append(settings, container.NewBorder(nil, nil, benchText(i18n.Current.BenchCodec, design.ColorTextMuted, 13, false), nil, codecSel))
	settings = append(settings, container.NewBorder(nil, nil, benchText(i18n.Current.BenchDuration, design.ColorTextMuted, 13, false), nil, durSel))

	hint := widget.NewLabel(i18n.Current.BenchHint)
	hint.Wrapping = fyne.TextWrapWord
	hint.Importance = widget.LowImportance
	content := container.New(&benchMinWidthLayout{width: 420}, benchCard(container.NewVBox(append([]fyne.CanvasObject{hint}, settings...)...), design.ColorBorder))
	view.ShowCustomConfirmDialog(i18n.Current.BenchTitle, i18n.Current.BenchStart, i18n.Current.Cancel, content, func(ok bool) {
		if !ok {
			benchmarkBusy.Store(false)
			return
		}
		var picked []string
		for _, kind := range []string{"sunshine", "rustshine"} {
			if checks[kind].Checked && !checks[kind].Disabled() {
				picked = append(picked, kind)
			}
		}
		if len(picked) == 0 {
			benchmarkBusy.Store(false)
			view.ShowInfoDialog(i18n.Current.BenchTitle, i18n.Current.BenchNeedOne, mw.window)
			return
		}
		window := benchmarkWindows[1].d
		for _, w := range benchmarkWindows {
			if w.label == durSel.Selected {
				window = w.d
			}
		}
		monitor := ""
		if monSel != nil {
			for _, c := range choices {
				if c.label == monSel.Selected {
					monitor = c.id
				}
			}
		}
		codec := ""
		for _, c := range benchCodecs {
			if c.label() == codecSel.Selected {
				codec = c.mode
			}
		}
		mw.startBenchmark(picked, monitor, codec, window)
	}, mw.window)
}

// benchCodec is one entry of the setup dialog's codec pick: the saved
// codec (mode ""), or one forced for every streamer so both are compared
// on the same codec.
type benchCodec struct {
	mode string
	name string
}

func (c benchCodec) label() string {
	if c.mode == "" {
		return i18n.Current.BenchCodecSaved
	}
	return c.name
}

var benchCodecs = []benchCodec{{"", ""}, {models.VideoModeH264, "H.264"}, {models.VideoModeH265, "HEVC (H.265)"}, {models.VideoModeAV1, "AV1"}}

func benchCodecLabels() []string {
	var out []string
	for _, c := range benchCodecs {
		out = append(out, c.label())
	}
	return out
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

// benchStyledPanel wraps body in the app's own dark bordered panel (bg
// design.ColorGray900, 1px design.ColorBorder, a centered title + an "X"
// close icon) instead of handing it to fyne's stock dialog.NewCustom. That
// package's panel background comes from theme.ColorNameOverlayBackground,
// which BrandTheme (design/theme.go) deliberately leaves transparent --
// every OTHER dialog in this app paints its own opaque panel as part of its
// content (see view.ShowCustomConfirmDialog), a convention dialog.NewCustom
// doesn't know about, so its panel showed through as the Fyne default
// (light) theme instead: white background, and this file's own
// design.ColorTextLight text on top of it read as white-on-white.
func benchStyledPanel(title string, body fyne.CanvasObject, onClose func()) fyne.CanvasObject {
	titleText := view.NewBrandText(title, 19, design.ColorTextLight, true)
	titleText.Alignment = fyne.TextAlignCenter
	closeBtn := widget.NewButtonWithIcon("", theme.CancelIcon(), onClose)
	closeBtn.Importance = widget.LowImportance
	titleBar := container.NewBorder(nil, nil, nil, closeBtn, titleText)

	bg := canvas.NewRectangle(design.ColorGray900)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = design.ColorBorder
	border.StrokeWidth = 1

	// Border, not VBox: a VBox only ever gives body its MinSize, which for
	// the results dialog's scroll is one line -- everything below "Saved
	// to" was clipped.
	content := container.NewBorder(titleBar, nil, nil, nil, view.NewInset(body, 0, 0, 16, 14))
	return container.NewStack(bg, view.NewInset(content, 18, 18, 16, 16), border)
}

// showBenchStyledDialog shows body in benchStyledPanel's dark panel via the
// app's own overlay popup machinery (view.ShowOverlayPopup), sized by
// sizeFn -- the "how big" policy the two callers below need differs (the
// progress dialog sizes to its own small content, the results dialog wants
// most of the window), so it's left to the caller rather than baked in
// here. onClose (may be nil) runs once, from the title bar's close icon,
// before the popup hides.
func showBenchStyledDialog(parent fyne.Window, title string, body fyne.CanvasObject,
	sizeFn func(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size, onClose func()) *widget.PopUp {
	var popup *widget.PopUp
	panel := benchStyledPanel(title, body, func() {
		if onClose != nil {
			onClose()
		}
		if popup != nil {
			popup.Hide()
		}
	})
	popup = view.ShowOverlayPopup(parent, view.OverlayPopupSpec{
		Panel:     panel,
		DimColor:  color.NRGBA{R: 0x00, G: 0x00, B: 0x00, A: 0x72},
		PanelSize: sizeFn,
	})
	return popup
}

// benchContentSizeFn sizes the panel to its own content (clamped to the
// available width) -- the progress dialog's status line + progress bar
// never need more room than that.
func benchContentSizeFn(canvasSize fyne.Size, panel fyne.CanvasObject) fyne.Size {
	const margin = 24
	maxWidth := canvasSize.Width - margin*2
	min := panel.MinSize()
	if min.Width > maxWidth {
		min.Width = maxWidth
	}
	return min
}

// benchResultsSizeFn sizes the results panel to most of the window
// regardless of its (large, scrollable) content's own MinSize -- matches
// the fyne dialog.NewCustom-based version's previous d.Resize(sz*0.94).
func benchResultsSizeFn(canvasSize fyne.Size, _ fyne.CanvasObject) fyne.Size {
	return fyne.NewSize(canvasSize.Width*0.94, canvasSize.Height*0.94)
}

func (mw *MainWindow) startBenchmark(backends []string, monitor, codec string, window time.Duration) {
	// No progress popup: the setup dialog closes on Start and nothing else
	// opens until the results. Any Fyne overlay over the stream hides the
	// native video (black picture on Windows, see
	// VideoWidget.syncCanvasOverlayHidden) -- exactly the stream being
	// measured -- so progress goes to the Net Graph HUD, which the
	// benchmark turns on anyway.
	ctx, cancel := context.WithCancel(context.Background())

	go func() {
		res, err := benchmarkRunFn(mw, ctx, backends, monitor, codec, window, func(text string, f float64) {
			service.SetNetGraphBanner(fmt.Sprintf("%s  %.0f%%", text, math.Min(math.Max(f, 0), 1)*100))
		})
		service.SetNetGraphBanner("")
		benchmarkBusy.Store(false)
		cancelled := ctx.Err() != nil
		cancel()
		if cancelled {
			return
		}
		if err != nil {
			fyne.Do(func() { view.ShowErrorDialog(fmt.Errorf("%s: %v", i18n.Current.BenchFailed, err), mw.window) })
			return
		}
		dir, err := saveBenchmarkResultFn(res)
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
	load := func(f func(service.BenchMetrics) float64) func(service.BenchMetrics) (float64, bool) {
		return func(m service.BenchMetrics) (float64, bool) { return f(m), m.HostLoadValid }
	}
	return []benchmarkRow{
		{"", "Codec", func(m service.BenchMetrics) (float64, bool) { return 0, m.Codec != "" }, "", true},

		{"Startup (measured separately)", "Host streamer switch", always(func(m service.BenchMetrics) float64 { return m.SwitchMs / 1000 }), "%.2f s", true},
		{"", "  stop previous streamer", func(m service.BenchMetrics) (float64, bool) { return m.StopMs / 1000, m.StopMs+m.StartMs > 0 }, "%.2f s", true},
		{"", "  start this streamer", func(m service.BenchMetrics) (float64, bool) { return m.StartMs / 1000, m.StopMs+m.StartMs > 0 }, "%.2f s", true},
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

		{"Host load (whole run, agent counters)", "GPU 3D, streamer", load(func(m service.BenchMetrics) float64 { return m.Streamer3DAvg }), "%.1f %%", true},
		{"", "GPU video encode, streamer", load(func(m service.BenchMetrics) float64 { return m.StreamerEncodeAvg }), "%.1f %%", true},
		{"", "GPU video decode, streamer", load(func(m service.BenchMetrics) float64 { return m.StreamerDecodeAvg }), "%.1f %%", true},
		{"", "CPU, streamer (all cores)", load(func(m service.BenchMetrics) float64 { return m.StreamerCPUAvg }), "%.1f %%", true},
		{"", "GPU 3D, whole host", load(func(m service.BenchMetrics) float64 { return m.GPU3DAvg }), "%.1f %%", true},
		{"", "GPU video encode, whole host", load(func(m service.BenchMetrics) float64 { return m.GPUEncodeAvg }), "%.1f %%", true},
		{"", "GPU video decode, whole host", load(func(m service.BenchMetrics) float64 { return m.GPUDecodeAvg }), "%.1f %%", true},
		{"", "CPU, whole host", load(func(m service.BenchMetrics) float64 { return m.HostCPUAvg }), "%.1f %%", true},

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

// benchBestColor marks the best value of a row or summary tile.
var benchBestColor = color.NRGBA{R: 0x4a, G: 0xd6, B: 0x6d, A: 0xff}

func benchText(s string, c color.Color, size float32, bold bool) *canvas.Text {
	t := canvas.NewText(s, c)
	t.TextSize = size
	t.TextStyle.Bold = bold
	return t
}

// benchCard is a rounded surface panel with an optional colored outline.
func benchCard(body fyne.CanvasObject, outline color.Color) fyne.CanvasObject {
	bg := canvas.NewRectangle(design.ColorSurface)
	bg.CornerRadius = design.RadiusMD
	border := canvas.NewRectangle(color.Transparent)
	border.CornerRadius = design.RadiusMD
	border.StrokeColor = outline
	border.StrokeWidth = 1
	return container.NewStack(bg, view.NewInset(body, 14, 14, 12, 12), border)
}

// benchSummaryTile is one metric in a streamer's summary card: a large
// value over a small caption, the value green when it's the best of all
// streamers.
type benchSummaryTile struct {
	caption string
	value   func(m service.BenchMetrics) (float64, bool)
	format  string
	lower   bool
}

var benchSummaryTiles = []benchSummaryTile{
	{"avg fps", func(m service.BenchMetrics) (float64, bool) { return m.AvgFPS, true }, "%.1f", false},
	{"1% low fps", func(m service.BenchMetrics) (float64, bool) { return m.Low1FPS, true }, "%.1f", false},
	{"stalls", func(m service.BenchMetrics) (float64, bool) { return float64(m.StallCount), true }, "%.0f", true},
	{"encode avg", func(m service.BenchMetrics) (float64, bool) { return m.HostLatencyAvg, true }, "%.1f ms", true},
	{"bitrate", func(m service.BenchMetrics) (float64, bool) { return m.BitrateMbps, true }, "%.1f Mbps", false},
	{"startup", func(m service.BenchMetrics) (float64, bool) { return (m.SwitchMs + m.StartupMs) / 1000, true }, "%.1f s", true},
}

// benchSummaryCards builds one card per streamer with its headline numbers
// side by side, so the comparison reads at a glance before the full table.
func benchSummaryCards(metrics []service.BenchMetrics) fyne.CanvasObject {
	best := make([]int, len(benchSummaryTiles))
	for t, tile := range benchSummaryTiles {
		vals := make([]float64, len(metrics))
		oks := make([]bool, len(metrics))
		for i, m := range metrics {
			vals[i], oks[i] = tile.value(m)
			oks[i] = oks[i] && m.Error == ""
		}
		best[t] = benchmarkBest(vals, oks, tile.lower)
	}
	var cards []fyne.CanvasObject
	for i, m := range metrics {
		accent := service.BenchBackendColor(m.Backend)
		title := benchText(service.BenchBackendLabel(m.Backend), accent, 17, true)
		sub := ""
		if m.Codec != "" {
			sub = strings.ToUpper(m.Codec)
		}
		head := container.NewBorder(nil, nil, title, benchText(sub, design.ColorTextMuted, 12, false))
		if m.Error != "" {
			msg := widget.NewLabel(m.Error)
			msg.Wrapping = fyne.TextWrapWord
			cards = append(cards, benchCard(container.NewVBox(head, benchText("failed", design.ColorDanger, 22, true), msg), accent))
			continue
		}
		var tiles []fyne.CanvasObject
		for t, tile := range benchSummaryTiles {
			v, ok := tile.value(m)
			s := "n/a"
			if ok {
				s = fmt.Sprintf(tile.format, v)
			}
			c := color.Color(design.ColorTextLight)
			if best[t] == i {
				c = benchBestColor
			}
			tiles = append(tiles, container.NewVBox(benchText(s, c, 22, true), benchText(tile.caption, design.ColorTextMuted, 11, false)))
		}
		cards = append(cards, benchCard(container.NewVBox(head, container.NewGridWithColumns(3, tiles...)), accent))
	}
	return container.NewGridWithColumns(len(cards), cards...)
}

// benchResultsTable is the full per-metric comparison: section header
// rows, then striped metric rows with the best value in green.
func benchResultsTable(metrics []service.BenchMetrics) fyne.CanvasObject {
	cols := 1 + len(metrics)
	row := func(cells []fyne.CanvasObject, fill color.Color) fyne.CanvasObject {
		grid := container.NewGridWithColumns(cols, cells...)
		if fill == nil {
			return view.NewInset(grid, 8, 8, 3, 3)
		}
		bg := canvas.NewRectangle(fill)
		bg.CornerRadius = 4
		return container.NewStack(bg, view.NewInset(grid, 8, 8, 3, 3))
	}

	head := []fyne.CanvasObject{benchText("", design.ColorTextLight, 13, true)}
	for _, m := range metrics {
		head = append(head, benchText(service.BenchBackendLabel(m.Backend), service.BenchBackendColor(m.Backend), 13, true))
	}
	rows := []fyne.CanvasObject{row(head, nil)}
	stripe := 0
	for _, r := range benchmarkRows() {
		if r.section != "" {
			rows = append(rows, view.NewInset(benchText(r.section, design.ColorAccent, 13, true), 8, 8, 12, 2), widget.NewSeparator())
			stripe = 0
		}
		cells := []fyne.CanvasObject{benchText(r.label, design.ColorTextMuted, 13, false)}
		vals := make([]float64, len(metrics))
		oks := make([]bool, len(metrics))
		for i, m := range metrics {
			vals[i], oks[i] = r.value(m)
			if m.Error != "" {
				oks[i] = false
			}
		}
		bestIdx := benchmarkBest(vals, oks, r.lower)
		for i, m := range metrics {
			s := "n/a"
			switch {
			case m.Error != "":
				s = "failed"
			case !oks[i]:
			case r.label == "Codec":
				s = strings.ToUpper(m.Codec)
			case r.label == "Frame time p50 / p95":
				s = fmt.Sprintf("%.1f / %.1f ms", m.IntervalP50, m.IntervalP95)
			case r.label == "Recovered by IDR / RFI":
				s = fmt.Sprintf("%d / %d", m.RecoveredByIDR, m.RecoveredByRFI)
			default:
				s = fmt.Sprintf(r.format, vals[i])
			}
			c := color.Color(design.ColorTextLight)
			if i == bestIdx {
				c = benchBestColor
			}
			cells = append(cells, benchText(s, c, 13, i == bestIdx))
		}
		var fill color.Color
		if stripe%2 == 1 {
			fill = design.ColorAlphaWhite07
		}
		stripe++
		rows = append(rows, row(cells, fill))
	}
	return container.NewVBox(rows...)
}

func benchStallList(metrics []service.BenchMetrics) fyne.CanvasObject {
	list := container.NewVBox()
	for _, m := range metrics {
		list.Add(benchText(service.BenchBackendLabel(m.Backend), service.BenchBackendColor(m.Backend), 13, true))
		if len(m.Stalls) == 0 {
			list.Add(benchText("   "+i18n.Current.BenchNoStalls, design.ColorTextMuted, 13, false))
		}
		for _, st := range m.Stalls {
			line := fmt.Sprintf("   %7.2fs  %5.0f ms  %s", st.AtMs/1000, st.DurationMs, st.Cause)
			if st.LostFrames > 0 {
				line += fmt.Sprintf(" (%d frames lost, recovered by %s)", st.LostFrames, strings.ToUpper(st.Recovery))
			}
			if st.Cause == service.BenchCauseHost {
				line += fmt.Sprintf(" (host captured nothing for %.0f ms)", st.HostGapMs)
			}
			list.Add(benchText(line, service.BenchCauseColor(st.Cause), 13, false))
		}
	}
	return list
}

func (mw *MainWindow) showBenchmarkResults(res *benchmarkResult, dir string) {
	metrics := res.Metrics

	sections := container.NewVBox()
	if len(res.Runs) > 0 && res.Runs[0].Content != "" {
		r := res.Runs[0]
		sections.Add(benchText(fmt.Sprintf("%s · %dx%d @ %d fps · %.0f s per streamer", r.Content, r.Width, r.Height, r.ExpectedFPS, res.Duration), design.ColorTextMuted, 12, false))
	}
	sections.Add(benchSummaryCards(metrics))
	sections.Add(benchCard(benchResultsTable(metrics), design.ColorBorder))

	img := service.RenderBenchChart(res.Runs, metrics)
	chart := canvas.NewImageFromImage(img)
	chart.FillMode = canvas.ImageFillContain
	w := float32(900)
	chart.SetMinSize(fyne.NewSize(w, w*float32(img.Bounds().Dy())/float32(img.Bounds().Dx())))
	sections.Add(benchCard(chart, design.ColorBorder))

	stalls := widget.NewAccordion(widget.NewAccordionItem(i18n.Current.BenchStalls, benchStallList(metrics)))
	sections.Add(stalls)

	scroll := container.NewVScroll(view.NewInset(sections, 0, 10, 0, 8))

	// Footer stays pinned below the scroll: where the auto-save went, and
	// the download/close actions.
	var popup *widget.PopUp
	saved := widget.NewLabel("")
	saved.Truncation = fyne.TextTruncateEllipsis
	if dir != "" {
		saved.SetText(fmt.Sprintf(i18n.Current.BenchSavedTo, dir))
	}
	download := widget.NewButtonWithIcon(i18n.Current.BenchSaveResults, theme.DownloadIcon(), func() {
		mw.downloadBenchmarkResults(res)
	})
	download.Importance = widget.HighImportance
	closeBtn := widget.NewButton(i18n.Current.Close, func() {
		if popup != nil {
			popup.Hide()
		}
	})
	footer := container.NewVBox(widget.NewSeparator(), container.NewBorder(nil, nil, nil, container.NewHBox(closeBtn, download), saved))

	popup = showBenchStyledDialog(mw.window, i18n.Current.BenchResultsTitle, container.NewBorder(nil, footer, nil, nil, scroll), benchResultsSizeFn, nil)
}

// downloadBenchmarkResults saves the results as one zip (results.json +
// chart.png) wherever the operator picks -- the auto-save location is a
// user-config directory most people never look inside.
func (mw *MainWindow) downloadBenchmarkResults(res *benchmarkResult) {
	fd := dialog.NewFileSave(func(wc fyne.URIWriteCloser, err error) {
		if err != nil || wc == nil {
			return
		}
		werr := writeBenchmarkZip(wc, res)
		if cerr := wc.Close(); werr == nil {
			werr = cerr
		}
		if werr != nil {
			view.ShowErrorDialog(fmt.Errorf(i18n.Current.BenchSaveResultsFailed, werr), mw.window)
			return
		}
		view.ShowInfoDialog(i18n.Current.BenchResultsTitle, fmt.Sprintf(i18n.Current.BenchSaveResultsDone, wc.URI().Name()), mw.window)
	}, mw.window)
	fd.SetFileName("usbridge-benchmark-" + res.CreatedAt.Format("20060102_150405") + ".zip")
	fd.Show()
}

// writeBenchmarkZip writes results.json and chart.png, the same pair
// saveBenchmarkResult stores, into one zip archive.
func writeBenchmarkZip(w io.Writer, res *benchmarkResult) error {
	zw := zip.NewWriter(w)
	data, err := json.MarshalIndent(res, "", " ")
	if err != nil {
		return err
	}
	f, err := zw.Create("results.json")
	if err != nil {
		return err
	}
	if _, err := f.Write(data); err != nil {
		return err
	}
	if f, err = zw.Create("chart.png"); err != nil {
		return err
	}
	if err := png.Encode(f, service.RenderBenchChart(res.Runs, res.Metrics)); err != nil {
		return err
	}
	return zw.Close()
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
