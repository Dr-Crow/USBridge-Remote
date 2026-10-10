package ui

import (
	"context"
	"errors"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/dialog"
	"fyne.io/fyne/v2/widget"

	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/sourcepreview"
)

// This deliberately separate action owns a same-user local preview. It never
// changes the saved streaming backend, enrolls a client, or exposes a network API.
func (w *Window) showSourcePreviewDialog(parent fyne.Window) {
	if w.sourcePreviewClose != nil {
		dialog.ShowInformation("Source preview", "Close the current preview dialog before opening another.", parent)
		return
	}
	if runtime.GOOS != "linux" {
		dialog.ShowInformation("Source preview", "This experimental viewer currently supports Linux X11 only.", parent)
		return
	}
	if w.token != nil && w.token.SessionActive() {
		dialog.ShowInformation("Source preview", "End the current streaming session before opening a local source preview.", parent)
		return
	}
	if w.sourcePreviewManager == nil {
		w.sourcePreviewManager = sourcepreview.New()
	}
	directory := widget.NewEntry()
	directory.SetPlaceHolder("Absolute source component directory")
	manifest := widget.NewEntry()
	manifest.SetPlaceHolder("Trusted manifest SHA-256")
	ffmpeg := widget.NewEntry()
	ffmpeg.SetPlaceHolder("Absolute trusted FFmpeg executable")
	display := widget.NewEntry()
	display.SetText(":0")
	profile := widget.NewSelect([]string{"128 × 72 (validated transport)", "640 × 360 (experimental content limits)"}, nil)
	profile.SetSelectedIndex(0)
	consent := widget.NewCheck("I approve capture of this selected local X11 display for this preview", nil)
	status := widget.NewLabel("No capture has started.")
	status.Wrapping = fyne.TextWrapWord
	scope := widget.NewLabel("Experimental, this computer only. The selected top-left pixel region at 30 FPS for up to 30 seconds. View-only with synthesized silence. FFmpeg and the verified native preview viewer must already be installed. Complex scenes may exceed the current encoder limit.")
	scope.Wrapping = fyne.TextWrapWord
	ctx, cancel := context.WithCancel(context.Background())
	var active *sourcepreview.Session
	var start, stop *widget.Button
	start = widget.NewButton("Start approved preview", func() {
		if !consent.Checked {
			status.SetText("Approve the exact display capture before starting.")
			return
		}
		if w.token != nil && w.token.SessionActive() {
			status.SetText("The stock session became active. Stop it before starting a preview.")
			return
		}
		approval := sourcepreview.Approval{Components: localcomponents.Options{Directory: strings.TrimSpace(directory.Text), ManifestSHA256: strings.TrimSpace(manifest.Text), StateDir: filepath.Join(w.cfg.StateDir, "source-preview")}, Display: strings.TrimSpace(display.Text), FFmpeg: strings.TrimSpace(ffmpeg.Text), CaptureConsent: true, VideoProfile: "transport-128"}
		if profile.SelectedIndex() == 1 {
			approval.VideoProfile = "desktop-640"
		}
		// A grant is consumed once. Every retry needs a fresh click and approval.
		consent.SetChecked(false)
		start.Disable()
		status.SetText("Verifying components and opening the local preview…")
		go func() {
			session, err := w.sourcePreviewManager.Start(ctx, approval)
			fyne.Do(func() {
				if err != nil {
					status.SetText(err.Error())
					if !errors.Is(err, sourcepreview.ErrCleanupUncertain) {
						start.Enable()
					}
					return
				}
				active = session
				stop.Enable()
				status.SetText("Connecting to the native viewer. Waiting for an actual frame…")
			})
			if err != nil {
				return
			}
			ticker := time.NewTicker(100 * time.Millisecond)
			defer ticker.Stop()
			last := ""
			for {
				select {
				case <-session.Done():
					err := session.Wait()
					fyne.Do(func() {
						if active == session {
							active = nil
							stop.Disable()
							if errors.Is(err, sourcepreview.ErrCleanupUncertain) {
								start.Disable()
								status.SetText(sourcepreview.ErrCleanupUncertain.Error())
							} else if errors.Is(err, sourcepreview.ErrFrameTooLarge) {
								start.Enable()
								status.SetText("This capture exceeded the bounded frame size. Select the smaller 128×72 profile, or use a verified bounded-encoding profile when available.")
							} else if err != nil {
								start.Enable()
								status.SetText("The preview failed and both children have stopped. Capture or transport limits may be responsible; inspect the selected display and tested profile before retrying.")
							} else {
								start.Enable()
								status.SetText("Preview stopped. Capture and viewer have been joined.")
							}
						}
					})
					return
				case <-ticker.C:
					state := session.State()
					if state != last {
						last = state
						if state == "viewing" {
							fyne.Do(func() {
								if active == session {
									status.SetText("Previewing actual frames. View-only; audio is synthesized silence.")
								}
							})
						}
					}
				}
			}
		}()
	})
	stop = widget.NewButton("Stop preview", func() {
		if active != nil {
			session := active
			stop.Disable()
			status.SetText("Stopping the viewer and capture…")
			go session.Stop()
		}
	})
	stop.Disable()
	form := widget.NewForm(widget.NewFormItem("Components", directory), widget.NewFormItem("Manifest SHA-256", manifest), widget.NewFormItem("FFmpeg", ffmpeg), widget.NewFormItem("X11 display", display), widget.NewFormItem("Capture profile", profile))
	content := container.NewVBox(scope, form, consent, container.NewHBox(start, stop), status)
	d := dialog.NewCustom("Source preview (experimental)", "Close and stop", content, parent)
	d.SetOnClosed(func() { cancel(); w.sourcePreviewClose = nil })
	w.sourcePreviewClose = func() { cancel(); d.Hide() }
	d.Resize(fyne.NewSize(640, 480))
	d.Show()
}

// The parent close-to-tray action and actual quit both revoke and join the
// capture lease. Hiding the main window must never leave preview capture alive.
func (w *Window) stopSourcePreview() {
	if w.sourcePreviewClose != nil {
		w.sourcePreviewClose()
	}
	if w.sourcePreviewManager != nil {
		_ = w.sourcePreviewManager.Stop()
	}
}
