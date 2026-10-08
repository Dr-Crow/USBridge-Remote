package ui

import (
	"errors"
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/canvas"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/widget"
	"image/color"
	"testing"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/ui/design"
)

type generalSettingsProvider struct {
	TokenProvider
	value string
	calls int
	err   error
}

func (p *generalSettingsProvider) StreamerAutoUpdateEnabled() bool { return false }
func (p *generalSettingsProvider) EntitlementStatus() entitlement.Status {
	return entitlement.Status{LocalRuntimeConfigured: true, LocalRuntimeActive: true, LocalRuntimeStreamerPrepared: true}
}
func (p *generalSettingsProvider) SetLocalWebClientURL(v string) error {
	p.calls++
	if p.err != nil {
		return p.err
	}
	p.value = v
	return nil
}

func TestLocalWebSettingSaveFailureAndClear(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	provider := &generalSettingsProvider{}
	w := &Window{app: a, token: provider, cfg: config.Default()}
	w.sunWebClientLink = newStatusLink("Configure local web client", design.ColorAddress, 10, nil)
	if err := w.saveLocalWebClient(" https://192.168.1.20/ "); err != nil {
		t.Fatal(err)
	}
	if w.cfg.LocalWebClientURL != provider.value || w.sunWebClientLink.label.Text != provider.value {
		t.Fatal("saved link not reflected immediately")
	}
	before := provider.calls
	if err := w.saveLocalWebClient("https://example.com/"); err == nil || provider.calls != before {
		t.Fatal("invalid public URL reached persistence")
	}
	provider.err = errors.New("fixture save failure")
	if err := w.saveLocalWebClient("https://192.168.1.21/"); err == nil {
		t.Fatal("failed save reported success")
	}
	if w.cfg.LocalWebClientURL != "https://192.168.1.20/" {
		t.Fatal("failed save replaced active URL")
	}
	provider.err = nil
	if err := w.saveLocalWebClient(""); err != nil {
		t.Fatal(err)
	}
	if w.cfg.LocalWebClientURL != "" || w.sunWebClientLink.label.Text != "Configure local web client" {
		t.Fatal("clear restored public fallback or stale label")
	}
	w.token = nil
	if err := w.saveLocalWebClient(""); err == nil {
		t.Fatal("unavailable engine silently accepted save")
	}
}

func findSettingsControls(root fyne.CanvasObject) (*widget.Entry, *widget.Button) {
	var entry *widget.Entry
	var save *widget.Button
	seen := map[fyne.CanvasObject]bool{}
	var walk func(fyne.CanvasObject)
	walk = func(o fyne.CanvasObject) {
		if o == nil || seen[o] {
			return
		}
		seen[o] = true
		if e, ok := o.(*widget.Entry); ok {
			entry = e
		}
		if b, ok := o.(*widget.Button); ok && b.Text == "Save local web client" {
			save = b
		}
		if c, ok := o.(*fyne.Container); ok {
			for _, child := range c.Objects {
				walk(child)
			}
		}
		if w, ok := o.(fyne.Widget); ok {
			for _, child := range test.WidgetRenderer(w).Objects() {
				walk(child)
			}
		}
	}
	walk(root)
	return entry, save
}
func TestGeneralSettingsSaveRemainsVisibleOnSmallCanvas(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(design.NewBrandTheme())
	win := a.NewWindow("settings")
	defer win.Close()
	provider := &generalSettingsProvider{}
	w := &Window{app: a, token: provider, cfg: config.Default()}
	panel := w.generalSettingsPanel(win, func() {})
	root := container.New(&overlayPopupLayout{}, canvas.NewRectangle(color.Transparent), panel)
	win.SetContent(root)
	win.Show()
	for _, size := range []fyne.Size{{Width: 1000, Height: 720}, {Width: 640, Height: 480}, {Width: 480, Height: 360}} {
		win.Resize(size)
		root.Resize(size)
		entry, save := findSettingsControls(panel)
		if entry == nil || save == nil {
			t.Fatal("local web controls missing")
		}
		pos := a.Driver().AbsolutePositionForObject(save)
		if pos.Y < 0 || pos.Y+save.Size().Height > size.Height || save.Size().Height <= 0 {
			t.Fatalf("save clipped for %v: %v %v", size, pos, save.Size())
		}
	}
	entry, save := findSettingsControls(panel)
	entry.SetText("https://192.168.1.30/")
	test.Tap(save)
	if provider.value != "https://192.168.1.30/" {
		t.Fatal("save button did not persist setting")
	}
	entry.SetText("https://192.168.1.31/")
	if provider.value != "https://192.168.1.30/" {
		t.Fatal("unsaved edit persisted")
	}
	reopened := w.generalSettingsPanel(win, func() {})
	entry2, _ := findSettingsControls(reopened)
	if entry2.Text != "https://192.168.1.30/" {
		t.Fatal("reopened form showed unsaved value")
	}
}
func TestGeneralSettingsParagraphHasBoundedInitialHeight(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	label := newGeneralSettingsParagraph("Local web client: use a private-IP HTTPS URL trusted by your browser. Blank disables the link in local mode. This does not configure the agent certificate.")
	if label.MinSize().Height > 180 {
		t.Fatalf("zero-width wrap inflated height: %v", label.MinSize())
	}
}
