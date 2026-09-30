package assets

import "fyne.io/fyne/v2"

// AWDL status icon: a minimal two-arc-and-dot Wi-Fi glyph (built from plain
// <circle>/<path> primitives, not a traced/downloaded svgrepo.com file like
// most of onboarding.go's icons -- there was no ready-made "AWDL/Wi-Fi
// mesh" glyph to embed, and a hand-built primitive shape is far less prone
// to a botched bezier reproduction than retyping a complex traced path from
// memory). Three states, matching the app's established Icon/IconActive/
// IconStatusBar color convention (see onboarding.go's NetworkIcon* for the
// pattern this mirrors):
//
//   - AWDLIcon (muted #8E8E8E, no slash): the "disable AWDL while
//     streaming" setting is off.
//   - AWDLIconActive (green #93C572, no slash): the setting is on, but
//     idle -- not currently streaming, so awdl0 hasn't been touched.
//   - AWDLIconStatusBar (lime #c4e77a, SLASHED): actively streaming right
//     now with awdl0 held down -- the slash reads as "AWDL is off",
//     matching what's actually true in this state.
var (
	AWDLIcon = fyne.NewStaticResource("awdl-status-off.svg", []byte(awdlIconSVG("#8E8E8E", false)))

	AWDLIconActive = fyne.NewStaticResource("awdl-status-active.svg", []byte(awdlIconSVG("#93C572", false)))

	AWDLIconStatusBar = fyne.NewStaticResource("awdl-status-suppressing.svg", []byte(awdlIconSVG("#c4e77a", true)))

	// AWDLIconFooterHover mirrors NetworkIconFooterHover's role -- the one
	// fixed hover-swap resource for this icon in the Control footer cluster
	// (headerStatusBadgeButton.SetHoverIcon sets a single resource
	// regardless of the icon's current on/off/active state, same as every
	// other footer icon here).
	AWDLIconFooterHover = fyne.NewStaticResource("awdl-status-footer-hover.svg", []byte(awdlIconSVG("#e0e3e7", false)))
)

// awdlIconSVG builds the glyph described above: a dot plus two concentric
// arcs (radius 5 and 9.8, both well within a valid range for their chord
// lengths of 7 and 14 respectively, so the arcs are always geometrically
// well-defined), optionally with a diagonal slash through it.
func awdlIconSVG(color string, slashed bool) string {
	slash := ""
	if slashed {
		slash = `<line x1="3.5" y1="3.5" x2="20.5" y2="20.5"/>`
	}
	return `<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="` + color + `" stroke-width="2" stroke-linecap="round" stroke-linejoin="round">` +
		`<circle cx="12" cy="19" r="1.3" fill="` + color + `" stroke="none"/>` +
		`<path d="M8.5 15.3a5 5 0 0 1 7 0"/>` +
		`<path d="M5 11.8a9.8 9.8 0 0 1 14 0"/>` +
		slash +
		`</svg>`
}
