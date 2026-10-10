//go:build linux && source_preview_acceptance

package ui

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"sync"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/app"
	// Read existing renderers for hit-target geometry only. Never use test.NewApp,
	// test.Tap, SetText, SetChecked, or invoke a widget callback in this fixture.
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/godbus/dbus/v5"

	"usbridge_agent/internal/account"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/sourcepreview"
	"usbridge_agent/internal/streamhost"
	"usbridge_agent/internal/tlshost"
	"usbridge_agent/internal/ui/design"
	"usbridge_agent/internal/usbpass"
)

// Only read methods used by the real parent window are implemented. All other
// methods retain a nil embedded interface and panic rather than reaching an
// account, service, permissions API, updater, vendor download, or real engine.
type previewOfflineEngine struct{ TokenProvider }

func (*previewOfflineEngine) AccountStatus() account.Status { return account.Status{} }
func (*previewOfflineEngine) EntitlementStatus() entitlement.Status {
	return entitlement.Status{ActiveBackend: "sunshine", LocalRuntimeConfigured: true, LocalRuntimeActive: true}
}
func (*previewOfflineEngine) SunshineCaptureMode() string                       { return "x11" }
func (*previewOfflineEngine) StreamerName() string                              { return "Sunshine" }
func (*previewOfflineEngine) SunshineStreamHost() string                        { return "127.0.0.1" }
func (*previewOfflineEngine) NvidiaPowerPrefsSupported() bool                   { return false }
func (*previewOfflineEngine) AWDLDisableDuringStreamingEnabled() bool           { return false }
func (*previewOfflineEngine) AWDLDisableDuringStreamingSupported() bool         { return false }
func (*previewOfflineEngine) ListSunshineClients() ([]streamhost.Client, error) { return nil, nil }
func (*previewOfflineEngine) USBPassthroughStatus() usbpass.Status              { return usbpass.Status{} }
func (*previewOfflineEngine) StreamerRunning() bool                             { return false }
func (*previewOfflineEngine) SessionActive() bool                               { return false }
func (*previewOfflineEngine) CertStatus() tlshost.CertStatus                    { return tlshost.CertStatus{} }

// Private-bus fixture host for the real StatusNotifierItem implementation.
// This is not a desktop tray renderer and makes no claim to test one.
type previewTrayWatcher struct {
	mu     sync.Mutex
	sender string
	path   string
}

func (t *previewTrayWatcher) RegisterStatusNotifierItem(service string, sender dbus.Sender) *dbus.Error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !strings.HasPrefix(service, "/") {
		return dbus.MakeFailedError(errors.New("expected item object path"))
	}
	t.sender, t.path = string(sender), service
	return nil
}
func (t *previewTrayWatcher) snapshot() (string, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sender, t.path
}

type previewHit struct {
	ID      string  `json:"id"`
	X       float32 `json:"x"`
	Y       float32 `json:"y"`
	Text    string  `json:"text,omitempty"`
	Enabled bool    `json:"enabled"`
	Checked bool    `json:"checked,omitempty"`
}

// RunSourcePreviewAcceptance runs the actual Window.ShowAndRun, with only its
// engine facade replaced. Its JSON output is a read-only accessibility-like
// view of existing widgets, never a command channel or synthetic UI status.
func RunSourcePreviewAcceptance() error {
	if os.Getenv("SOURCE_PREVIEW_DIALOG_ACCEPTANCE") != "1" || os.Getenv("DISPLAY") != ":97" || os.Geteuid() == 0 {
		return errors.New("requires unprivileged isolated dialog acceptance on :97")
	}
	root := os.Getenv("SOURCE_PREVIEW_DIALOG_WORK")
	if !filepath.IsAbs(root) || os.Getenv("HOME") != filepath.Join(root, "home") || os.Getenv("XDG_CONFIG_HOME") != filepath.Join(root, "config") {
		return errors.New("isolated HOME and XDG config required")
	}
	// The runner provides an isolated network namespace and a fresh session bus.
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return err
	}
	defer conn.Close()
	watcher := &previewTrayWatcher{}
	if err = conn.Export(watcher, "/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher"); err != nil {
		return err
	}
	reply, err := conn.RequestName("org.kde.StatusNotifierWatcher", dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		return errors.New("private tray watcher already owned")
	}
	a := app.NewWithID("io.usbridge.source-preview-native-acceptance")
	if reflect.TypeOf(a.Driver()).Elem().PkgPath() != "fyne.io/fyne/v2/internal/driver/glfw" {
		return errors.New("acceptance requires native GLFW driver; do not build with -tags ci")
	}
	a.Settings().SetTheme(design.NewBrandTheme())
	cfg := config.Default()
	cfg.StateDir = filepath.Join(root, "state")
	cfg.TailscaleEnabled = false
	cfg.ListenHost = "127.0.0.1"
	w := NewWindow(a, cfg, nil, nil, &previewOfflineEngine{})
	manager, observe := sourcepreview.NewAcceptanceObservedManager()
	w.sourcePreviewManager = manager
	defer manager.Stop()
	done := make(chan struct{})
	defer close(done)
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				fyne.Do(func() {
					if w.guiWin == nil {
						return
					}
					sender, path := watcher.snapshot()
					record := map[string]any{"pid": os.Getpid(), "title": w.guiWin.Title(), "native_glfw": true, "tray_attached": w.tray != nil, "tray_sender": sender, "tray_path": path, "dialog_open": w.sourcePreviewClose != nil, "controls": previewHits(a, w.guiWin), "launches": observe()}
					raw, e := json.Marshal(record)
					if e != nil {
						panic(e)
					}
					tmp := filepath.Join(root, "ui.json.tmp")
					if e = os.WriteFile(tmp, append(raw, '\n'), 0600); e != nil {
						panic(e)
					}
					if e = os.Rename(tmp, filepath.Join(root, "ui.json")); e != nil {
						panic(e)
					}
				})
			}
		}
	}()
	w.ShowAndRun(func() { a.Quit() })
	return nil
}

func previewHits(a fyne.App, win fyne.Window) []previewHit {
	out := []previewHit{}
	seen := map[fyne.CanvasObject]bool{}
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || seen[o] || !o.Visible() {
			return
		}
		seen[o] = true
		hit := previewHit{Enabled: true}
		switch c := o.(type) {
		case *headerIconButton:
			if c.icon != nil && c.icon.Name() == theme.SettingsIcon().Name() {
				hit.ID = "settings"
			}
		case *tealMenuRow:
			if c.label == "Source preview (experimental)" {
				hit.ID = "source-menu"
				hit.Text = c.label
				hit.Enabled = !c.disabled
			}
		case *widget.Entry:
			switch c.PlaceHolder {
			case "Absolute source component directory":
				hit.ID = "components"
			case "Trusted manifest SHA-256":
				hit.ID = "manifest"
			case "Absolute trusted FFmpeg executable":
				hit.ID = "ffmpeg"
			default:
				// The shipped display entry is the sole unlabelled entry in this dialog.
				if strings.HasPrefix(c.Text, ":") {
					hit.ID = "display"
				}
			}
			hit.Text = c.Text
			hit.Enabled = !c.Disabled()
		case *widget.Button:
			switch c.Text {
			case "Start approved preview":
				hit.ID = "start"
			case "Stop preview":
				hit.ID = "stop"
			case "Close and stop":
				hit.ID = "close-dialog"
			}
			hit.Text = c.Text
			hit.Enabled = !c.Disabled()
		case *widget.Check:
			if c.Text == "I approve capture of this selected local X11 display for this preview" {
				hit.ID = "consent"
				hit.Checked = c.Checked
				hit.Enabled = !c.Disabled()
			}
		case *widget.Select:
			if len(c.Options) == 2 && c.Options[0] == "128 × 72 (validated transport)" {
				hit.ID = "profile"
				hit.Text = c.Selected
				hit.Enabled = !c.Disabled()
			}
		case *widget.Label:
			for _, prefix := range []string{"No capture", "Approve the exact", "Verifying components", "verified preview viewer", "Connecting to the native", "Previewing actual frames", "Stopping the viewer", "Preview stopped", "The preview failed", "source preview could not", "preview renderer could not"} {
				if strings.HasPrefix(c.Text, prefix) {
					hit.ID = "status"
					hit.Text = c.Text
					break
				}
			}
		}
		if hit.ID != "" {
			p := a.Driver().AbsolutePositionForObject(o)
			sz := o.Size()
			scale := win.Canvas().Scale()
			hit.X = (p.X + sz.Width/2) * scale
			hit.Y = (p.Y + sz.Height/2) * scale
			if hit.ID == "consent" {
				hit.X = (p.X + 12) * scale
			}
			if sz.Width > 0 && sz.Height > 0 {
				out = append(out, hit)
			}
		}
		if c, ok := o.(*fyne.Container); ok {
			for _, child := range c.Objects {
				walk(child)
			}
		}
		if wid, ok := o.(fyne.Widget); ok {
			for _, child := range test.WidgetRenderer(wid).Objects() {
				walk(child)
			}
		}
	}
	walk(win.Content())
	for _, o := range win.Canvas().Overlays().List() {
		walk(o)
	}
	// Duplicate identifiers must fail the driver instead of selecting a lookalike.
	counts := map[string]int{}
	for _, h := range out {
		counts[h.ID]++
		if counts[h.ID] > 1 {
			panic(fmt.Sprintf("ambiguous acceptance target %q", h.ID))
		}
	}
	return out
}
