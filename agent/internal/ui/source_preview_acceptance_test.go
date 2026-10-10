//go:build linux && source_preview_acceptance

package ui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/test"
	"fyne.io/fyne/v2/theme"
	"testing"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/ui/design"
)

// A locator unit test only, not native acceptance or a full-parent smoke test.
// Do not run ShowAndRun on Fyne's headless driver: its immediate fyne.Do calls
// cannot model the native main-thread scheduling of the parent's refresh loop.
func TestSourcePreviewAcceptanceDialogLocator(t *testing.T) {
	a := test.NewApp()
	defer a.Quit()
	a.Settings().SetTheme(design.NewBrandTheme())
	cfg := config.Default()
	cfg.StateDir = t.TempDir()
	w := NewWindow(a, cfg, nil, nil, &previewOfflineEngine{})
	win := a.NewWindow("locator unit test")
	defer win.Close()
	var settings *headerIconButton
	settings = newHeaderIconButton(theme.SettingsIcon(), func() { w.showSettingsMenu(win, settings) })
	win.SetContent(container.NewVBox(settings))
	win.Resize(fyne.NewSize(1000, 800))
	win.Show()
	has := func(id string) bool {
		for _, h := range previewHits(a, win) {
			if h.ID == id {
				return true
			}
		}
		return false
	}
	if !has("settings") {
		t.Fatal("settings button not discoverable")
	}
	w.showSettingsMenu(win, settings)
	if !has("source-menu") {
		t.Fatal("shipped source menu item not discoverable")
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
			t.Fatalf("shipped dialog target %s not discoverable", id)
		}
	}
}
