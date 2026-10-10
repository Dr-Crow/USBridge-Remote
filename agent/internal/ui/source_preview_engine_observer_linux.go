//go:build linux && source_preview_engine_acceptance

package ui

// Read-only, opt-in metadata attached to the shipped Window in the actual CLI.
// It never constructs/replaces an engine or manager, invokes callbacks, grants
// consent, supplies keys, or changes configuration. Normal builds use a no-op.
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
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/theme"
	"fyne.io/fyne/v2/widget"
	"github.com/godbus/dbus/v5"
)

type enginePreviewWatcher struct {
	mu           sync.Mutex
	sender, path string
}

func (t *enginePreviewWatcher) RegisterStatusNotifierItem(service string, sender dbus.Sender) *dbus.Error {
	t.mu.Lock()
	defer t.mu.Unlock()
	if !strings.HasPrefix(service, "/") {
		return dbus.MakeFailedError(errors.New("expected item object path"))
	}
	t.sender, t.path = string(sender), service
	return nil
}
func (t *enginePreviewWatcher) snapshot() (string, string) {
	t.mu.Lock()
	defer t.mu.Unlock()
	return t.sender, t.path
}

func observeSourcePreviewEngine(w *Window) func() {
	if os.Getenv("SOURCE_PREVIEW_ENGINE_ACCEPTANCE") != "1" {
		return func() {}
	}
	root := os.Getenv("SOURCE_PREVIEW_ENGINE_WORK")
	if os.Geteuid() == 0 || root != "/work" || os.Getenv("HOME") != "/work/home" || os.Getenv("DISPLAY") != ":97" {
		panic("engine observer requires disposable unprivileged acceptance container")
	}
	typ := reflect.TypeOf(w.token)
	if typ == nil || typ.Kind() != reflect.Pointer || typ.Elem().PkgPath() != "usbridge_agent/internal/app" || typ.Elem().Name() != "App" || !w.ownsEngine {
		panic("engine acceptance requires the actual owned App")
	}
	if reflect.TypeOf(w.app.Driver()).Elem().PkgPath() != "fyne.io/fyne/v2/internal/driver/glfw" {
		panic("engine acceptance requires the native GLFW driver")
	}
	engine, ok := w.token.(interface{ SourcePreviewEngineMetadata() map[string]any })
	if !ok {
		panic("engine metadata unavailable")
	}
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		panic(err)
	}
	watcher := &enginePreviewWatcher{}
	if err = conn.Export(watcher, "/StatusNotifierWatcher", "org.kde.StatusNotifierWatcher"); err != nil {
		panic(err)
	}
	reply, err := conn.RequestName("org.kde.StatusNotifierWatcher", dbus.NameFlagDoNotQueue)
	if err != nil || reply != dbus.RequestNameReplyPrimaryOwner {
		panic("private fixture tray host unavailable")
	}
	done := make(chan struct{})
	go func() {
		tick := time.NewTicker(100 * time.Millisecond)
		defer tick.Stop()
		for {
			select {
			case <-done:
				return
			case <-tick.C:
				fyne.Do(func() {
					select {
					case <-done:
						return
					default:
					}
					sender, path := watcher.snapshot()
					record := map[string]any{"pid": os.Getpid(), "title": w.guiWin.Title(), "native_glfw": true, "instrumentation": "source_preview_engine_acceptance/read-only-v1", "owns_engine": w.ownsEngine, "tray_attached": w.tray != nil, "tray_sender": sender, "tray_path": path, "dialog_open": w.sourcePreviewClose != nil, "controls": enginePreviewHits(w.app, w.guiWin), "engine": engine.SourcePreviewEngineMetadata()}
					raw, err := json.Marshal(record)
					if err != nil {
						panic(err)
					}
					tmp := filepath.Join(root, "ui.json.tmp")
					if err = os.WriteFile(tmp, append(raw, '\n'), 0600); err != nil {
						panic(err)
					}
					if err = os.Rename(tmp, filepath.Join(root, "ui.json")); err != nil {
						panic(err)
					}
				})
			}
		}
	}()
	return func() { close(done); conn.Close() }
}

type enginePreviewHit struct {
	ID      string  `json:"id"`
	X       float32 `json:"x"`
	Y       float32 `json:"y"`
	Text    string  `json:"text,omitempty"`
	Enabled bool    `json:"enabled"`
	Checked bool    `json:"checked,omitempty"`
}

func enginePreviewHits(a fyne.App, win fyne.Window) []enginePreviewHit {
	out := []enginePreviewHit{}
	seen := map[fyne.CanvasObject]bool{}
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || seen[o] || !o.Visible() {
			return
		}
		seen[o] = true
		hit := enginePreviewHit{Enabled: true}
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
		// Read existing content/model links only. Do not depend directly on
		// Fyne test helpers, initialize an app, or create another renderer.
		switch c := o.(type) {
		case *container.Scroll:
			walk(c.Content)
		case *widget.PopUp:
			walk(c.Content)
		case *widget.Form:
			for _, item := range c.Items {
				walk(item.Widget)
			}
		case *tealMenuPopup:
			walk(c.content)
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
