package gui

import (
	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"usbridge-client/internal/gui/controller"
	"usbridge-client/internal/gui/i18n"
)

// wireVideoHotkeys connects the video widget's Ctrl+Alt+Shift hotkeys
// (controller/video_widget_hotkeys.go) to main-window state.
func (mw *MainWindow) wireVideoHotkeys() {
	if mw.videoWidget == nil {
		return
	}
	mw.videoWidget.SetHotkeyActions(controller.HotkeyActions{
		ToggleMouseMode: func() {
			if mw.diskWidget == nil {
				return
			}
			if mw.diskWidget.GetMouseMode() == controller.MouseModeAbsolute {
				mw.diskWidget.SetMouseMode(controller.MouseModeTouchPad)
			} else {
				mw.diskWidget.SetMouseMode(controller.MouseModeAbsolute)
			}
		},
		ToggleShowMouse: mw.toggleShowMouse,
	})
}

// toggleShowMouse flips the persisted Show Mouse setting (mouse menu,
// Ctrl+Alt+Shift+N) and applies it to the stream.
func (mw *MainWindow) toggleShowMouse() {
	next := !mw.app.Preferences().BoolWithFallback("show_mouse_cursor", false)
	mw.app.Preferences().SetBool("show_mouse_cursor", next)
	if mw.videoWidget != nil {
		mw.videoWidget.SetShowMouseCursor(next)
	}
}

// showHotkeysDialog lists the hotkeys (gear menu → Hotkeys).
func (mw *MainWindow) showHotkeysDialog() {
	rows := []struct{ key, what string }{
		{"Q", i18n.Current.HotkeyQuit},
		{"X", i18n.Current.HotkeyFullscreen},
		{"S", i18n.Current.HotkeyStats},
		{"M", i18n.Current.HotkeyMouseMode},
		{"N", i18n.Current.HotkeyCursor},
		{"V", i18n.Current.HotkeyPaste},
		{"F1 … F12", i18n.Current.HotkeyDisplays},
	}
	var cells []fyne.CanvasObject
	for _, r := range rows {
		k := widget.NewLabelWithStyle("Ctrl+Alt+Shift+"+r.key, fyne.TextAlignLeading, fyne.TextStyle{Bold: true, Monospace: true})
		cells = append(cells, k, widget.NewLabel(r.what))
	}
	note := widget.NewLabel(i18n.Current.HotkeysNote)
	note.Wrapping = fyne.TextWrapWord
	content := container.NewVBox(container.NewGridWithColumns(2, cells...), widget.NewSeparator(), note)
	d := dialog.NewCustom(i18n.Current.HotkeysTitle, i18n.Current.Close, content, mw.window)
	d.Resize(fyne.NewSize(640, 0))
	d.Show()
}
