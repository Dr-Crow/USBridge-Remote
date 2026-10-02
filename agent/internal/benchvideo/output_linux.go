//go:build linux

package benchvideo

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/godbus/dbus/v5"
)

const (
	kwinReportPath  = "/io/usbridge/BenchPlacement"
	kwinReportIface = "io.usbridge.BenchPlacement"
	kwinPluginName  = "usbridge-bench-placement"
)

// kwinReport receives the placement script's one-line result.
type kwinReport chan string

func (r kwinReport) Report(result string) *dbus.Error {
	select {
	case r <- result:
	default:
	}
	return nil
}

// moveToOutput puts pid's windows on the compositor output whose name
// starts with prefix and returns that output's name. KWin only: a Wayland
// client can't place its own window on an output, and none of the players
// here has a switch that names one KWin made up at runtime, so a script run
// inside KWin moves the window instead. The script answers over D-Bus,
// since KWin gives a script no other way to return a value.
func moveToOutput(pid int, prefix string, timeout time.Duration) (string, error) {
	conn, err := dbus.ConnectSessionBus()
	if err != nil {
		return "", fmt.Errorf("session bus: %w", err)
	}
	defer conn.Close()

	report := make(kwinReport, 1)
	if err := conn.Export(report, kwinReportPath, kwinReportIface); err != nil {
		return "", err
	}
	dir, err := os.MkdirTemp("", "usbridge-bench-")
	if err != nil {
		return "", err
	}
	defer os.RemoveAll(dir)
	script := filepath.Join(dir, "place.js")
	if err := os.WriteFile(script, []byte(kwinPlacementScript(conn.Names()[0], pid, prefix)), 0o644); err != nil {
		return "", err
	}

	scripting := conn.Object("org.kde.KWin", "/Scripting")
	last := "no answer from KWin"
	for deadline := time.Now().Add(timeout); ; {
		// A script left loaded under this name (an agent killed mid-run)
		// would make loadScript refuse the new one.
		_ = scripting.Call("org.kde.kwin.Scripting.unloadScript", 0, kwinPluginName).Err
		var id int32
		if err := scripting.Call("org.kde.kwin.Scripting.loadScript", 0, script, kwinPluginName).Store(&id); err != nil {
			return "", fmt.Errorf("KWin scripting is not available (the test video can only be moved to a virtual display on KDE): %w", err)
		}
		if id < 0 {
			return "", errors.New("KWin refused the placement script")
		}
		run := conn.Object("org.kde.KWin", dbus.ObjectPath(fmt.Sprintf("/Scripting/Script%d", id)))
		if err := run.Call("org.kde.kwin.Script.run", 0).Err; err != nil {
			return "", fmt.Errorf("running the placement script: %w", err)
		}
		select {
		case last = <-report:
		case <-time.After(2 * time.Second):
		}
		_ = scripting.Call("org.kde.kwin.Scripting.unloadScript", 0, kwinPluginName).Err

		if name, ok := strings.CutPrefix(last, "moved "); ok {
			if !strings.HasPrefix(name, prefix) {
				return "", fmt.Errorf("KWin left the player on %q", name)
			}
			return name, nil
		}
		// "no-window": the player hasn't mapped its window yet.
		// "no-output": the streamer hasn't created its display yet.
		if time.Now().After(deadline) {
			break
		}
		time.Sleep(250 * time.Millisecond)
	}
	switch last {
	case "no-output":
		return "", fmt.Errorf("there is no %s* display to play on (is a client streaming?)", prefix)
	case "no-window":
		return "", errors.New("the player never opened a window")
	}
	return "", errors.New(last)
}

// kwinPlacementScript is KWin 6 JavaScript. A fullscreen window is taken
// out of fullscreen for the move and put back, which is what makes it fill
// the new output rather than keep the old one's geometry.
func kwinPlacementScript(service string, pid int, prefix string) string {
	return fmt.Sprintf(`(function () {
    function report(s) { callDBus(%q, %q, %q, "Report", s); }
    var out = null, screens = workspace.screens;
    for (var i = 0; i < screens.length; i++) {
        if (screens[i].name.indexOf(%q) === 0) { out = screens[i]; break; }
    }
    if (!out) { report("no-output"); return; }
    var wins = workspace.windowList(), landed = null;
    for (var j = 0; j < wins.length; j++) {
        var w = wins[j];
        if (w.pid !== %d) continue;
        var fs = w.fullScreen;
        if (fs) w.fullScreen = false;
        workspace.sendClientToScreen(w, out);
        if (fs) w.fullScreen = true;
        // Where the window really is now, not where it was sent.
        landed = w.output ? w.output.name : "";
    }
    report(landed === null ? "no-window" : "moved " + landed);
})();
`, service, kwinReportPath, kwinReportIface, prefix, pid)
}
