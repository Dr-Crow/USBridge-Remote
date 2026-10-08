package ui

import (
	"fmt"

	"fyne.io/fyne/v2"
	"fyne.io/fyne/v2/container"
	"fyne.io/fyne/v2/widget"
	"github.com/sirupsen/logrus"

	"usbridge_agent/internal/entitlement"
)

const generalSettingsDialogWidth float32 = 380

// Shared left/right inset so General Settings rows keep labels and
// checkboxes in one column.
const generalSettingsRowInset float32 = 8

// showGeneralSettingsDialog is Settings → General Settings: shared agent
// preferences. Auto-update is persisted on the engine (config.yaml) so a
// headless service honors it too.
//
// The remote-window-lock toggle is intentionally hidden for now (Linux
// path still needs polish before release); config/API remain for later.
func (w *Window) showGeneralSettingsDialog(parent fyne.Window) {
	if parent == nil {
		parent = w.guiWin
	}
	if parent == nil {
		return
	}

	enabled := true
	if w.token != nil {
		enabled = w.token.StreamerAutoUpdateEnabled()
	}

	var check *styledCheck
	check = newStyledCheck("", enabled, func(on bool) {
		if w.token == nil {
			return
		}
		if err := w.token.SetStreamerAutoUpdate(on); err != nil {
			logrus.WithError(err).Warn("could not save streamer auto-update")
			check.SetChecked(!on)
		}
	})
	st := entitlement.Status{}
	if w.token != nil {
		st = w.token.EntitlementStatus()
	}
	diagnostics := widget.NewLabel(localRuntimeDiagnostics(st))
	diagnostics.Wrapping = fyne.TextWrapWord
	instructions := widget.NewLabel("On supported platforms, normal launches automatically enable the fork runtime. Select your streamer as usual; supported component copies are prepared before launch. USB setup and OS permissions remain separate. Changing this advanced override requires an engine restart. Unsupported component versions are refused rather than silently patched.")
	instructions.Wrapping = fyne.TextWrapWord
	var runtimeCheck *styledCheck
	saveRuntime := func(on bool) {
		if err := w.token.SetLocalRuntimeEnabled(on); err != nil {
			runtimeCheck.SetChecked(!on)
			showErrorDialog(fmt.Errorf("save local runtime preference: %w", err), parent)
			return
		}
		diagnostics.SetText(localRuntimeDiagnostics(w.token.EntitlementStatus()))
		w.refreshProtocolPickerVisuals(false)
	}
	runtimeCheck = newStyledCheck("", st.LocalRuntimeConfigured, func(on bool) {
		if w.token == nil {
			return
		}
		if !on {
			saveRuntime(false)
			return
		}
		runtimeCheck.Disable()
		showConfirmDialog("Enable experimental local runtime?", "This takes effect on the next engine start and uses modified, hash-pinned research copies. Downloads still require genuine authorization; USB setup and device permissions remain separate.", func(yes bool) {
			runtimeCheck.Enable()
			if !yes {
				runtimeCheck.SetChecked(false)
				return
			}
			saveRuntime(true)
		}, parent)
	})
	refresh := widget.NewButton("Refresh setup diagnostics", func() {
		if w.token != nil {
			diagnostics.SetText(localRuntimeDiagnostics(w.token.EntitlementStatus()))
		}
	})
	body := container.New(&tightVBoxLayout{gap: 10},
		newExactInset(newPermToggleRow(loc().AgentAutoUpdate, check), generalSettingsRowInset, generalSettingsRowInset, 0, 0),
		newExactInset(newPermToggleRow("Local runtime (advanced override)", runtimeCheck), generalSettingsRowInset, generalSettingsRowInset, 0, 0),
		instructions, diagnostics, refresh,
	)

	var popup *widget.PopUp
	closeDialog := func() {
		if popup != nil {
			popup.Hide()
		}
	}
	panel := newBrandedDialogPanelChrome(loc().GeneralSettings, loc().GeneralSettingsSubtitle, generalSettingsDialogWidth, 20, 8, body, nil, closeDialog)
	popup = showOverlayPopup(parent, overlayPopupSpec{Panel: panel})
}

// Safe setup facts only: no credentials, identifiers, paths or login URLs.
func localRuntimeDiagnostics(st entitlement.Status) string {
	mode := "Vendor runtime active."
	if st.LocalRuntimeActive {
		mode = "Local runtime active (experimental)."
	}
	if st.LocalRuntimeConfigured != st.LocalRuntimeActive {
		mode += " Saved preference differs: restart engine to apply. A CLI/environment override must also be removed to disable it."
	}
	streamer := "not downloaded"
	if st.RustShineStaged {
		streamer = "downloaded; local copy not prepared"
	}
	if st.LocalRuntimeStreamerPrepared {
		streamer = "patched copy prepared this session"
	}
	usb := "local copy not prepared (USB consent remains separate)"
	if st.LocalRuntimeUSBPrepared {
		usb = "patched copy prepared this session"
	}
	return fmt.Sprintf("%s\nStreamer: %s.\nUSB broker: %s.\nPrepared does not confirm an active stream or working tablet.", mode, streamer, usb)
}
