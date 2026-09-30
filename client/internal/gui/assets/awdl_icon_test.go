package assets

import (
	"encoding/xml"
	"testing"
)

// TestAWDLIconSVGWellFormed guards against a hand-typed markup mistake in
// awdlIconSVG (no visual rendering tool is available to eyeball these) --
// at minimum, catches unclosed tags/attributes before they'd otherwise only
// surface as a blank or broken icon at runtime.
func TestAWDLIconSVGWellFormed(t *testing.T) {
	for _, slashed := range []bool{false, true} {
		svg := awdlIconSVG("#8E8E8E", slashed)
		var doc struct {
			XMLName xml.Name `xml:"svg"`
		}
		if err := xml.Unmarshal([]byte(svg), &doc); err != nil {
			t.Fatalf("awdlIconSVG(slashed=%v) is not well-formed XML: %v\n%s", slashed, err, svg)
		}
	}
}

func TestAWDLIconResourcesNonEmpty(t *testing.T) {
	for name, res := range map[string]interface{ Content() []byte }{
		"AWDLIcon":            AWDLIcon,
		"AWDLIconActive":      AWDLIconActive,
		"AWDLIconStatusBar":   AWDLIconStatusBar,
		"AWDLIconFooterHover": AWDLIconFooterHover,
	} {
		if len(res.Content()) == 0 {
			t.Errorf("%s has empty content", name)
		}
	}
}
