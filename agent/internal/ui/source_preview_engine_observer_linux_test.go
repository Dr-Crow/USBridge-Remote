//go:build linux && source_preview_engine_acceptance

package ui

import (
	"testing"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/ui/design"
)

func TestEnginePreviewObserverOptIn(t *testing.T) {
	t.Setenv("SOURCE_PREVIEW_ENGINE_ACCEPTANCE", "")
	// Nil is safe only because the hook must do nothing without explicit opt-in.
	observeSourcePreviewEngine(nil)()
}

// Locator unit coverage only: not evidence of a native renderer or App.New.
func TestEnginePreviewNativeControlLocator(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(design.NewBrandTheme())
	cfg := config.Default()
	cfg.StateDir = t.TempDir()
	w := NewWindow(a, cfg, nil, nil, nil)
	win := a.NewWindow("locator unit test")
	defer win.Close()
	var settings *headerIconButton
	settings = newHeaderIconButton(theme.SettingsIcon(), func() { w.showSettingsMenu(win, settings) })
	win.SetContent(container.NewVScroll(container.NewVBox(settings)))
	win.Resize(fyne.NewSize(1000, 800))
	win.Show()
	has := func(id string) bool {
		for _, h := range enginePreviewHits(a, win) {
			if h.ID == id {
				return true
			}
		}
		return false
	}
	if !has("settings") {
		t.Fatal("missing actual settings target")
	}
	w.showSettingsMenu(win, settings)
	if !has("source-menu") {
		t.Fatal("missing actual Source menu target")
	}
	for _, overlay := range win.Canvas().Overlays().List() {
		if popup, ok := overlay.(*tealMenuPopup); ok {
			popup.Hide()
		}
	}
	w.showSourcePreviewDialog(win)
	defer w.stopSourcePreview()
	for _, id := range []string{"components", "manifest", "ffmpeg", "display", "profile", "consent", "start", "stop", "close-dialog", "status"} {
		if !has(id) {
			t.Fatalf("missing actual dialog target %s", id)
		}
	}
}
