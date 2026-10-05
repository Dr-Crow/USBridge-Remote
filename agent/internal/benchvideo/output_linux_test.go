//go:build linux

package benchvideo

import (
	"strings"
	"testing"
)

// The script must act on this player and this display only, and answer the
// connection that asked.
func TestKWinPlacementScript(t *testing.T) {
	js := kwinPlacementScript(":1.234", 4242, "Virtual-punktfunk")
	for _, want := range []string{
		`callDBus(":1.234", "/io/usbridge/BenchPlacement", "io.usbridge.BenchPlacement", "Report", s)`,
		`indexOf("Virtual-punktfunk") === 0`,
		`w.pid !== 4242`,
		`workspace.sendClientToScreen(w, out)`,
		`report("no-output")`,
	} {
		if !strings.Contains(js, want) {
			t.Errorf("script lacks %s\n%s", want, js)
		}
	}
}
