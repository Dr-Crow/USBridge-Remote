package streamhost

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"runtime"
	"strings"
)

// ListCaptureDevices lists the host's physical monitors as punktfunk-host
// itself sees them (`punktfunk-host list-monitors`, Linux only), for
// SetOutputName. Empty when there is no binary, off Linux, or when the
// compositor isn't reachable.
//
// On KDE, listing works for any binary but capturing doesn't: KWin only
// hands zkde_screencast_unstable_v1 to a punktfunk-host whose executable
// path is the Exec= of an installed .desktop file listing that interface in
// X-KDE-Wayland-Interfaces (punktfunk's packages install
// io.unom.Punktfunk.Host.desktop for /usr/bin/punktfunk-host). Without it a
// session fails with "KWin does not expose zkde_screencast_unstable_v1 to
// this client" -- confirmed live, and fixed by a user-level .desktop naming
// the binary's real path.
func (b *punktfunkBackend) ListCaptureDevices() []CaptureDevice {
	// list-monitors is a Linux command: elsewhere punktfunk-host answers
	// "unknown command". On Windows every call of it also flashed a console
	// window, and the GUI asks for devices every few seconds.
	if runtime.GOOS != "linux" {
		return nil
	}
	bin := b.binaryPath()
	if bin == "" {
		return nil
	}
	cmd := exec.Command(bin, "list-monitors")
	configureProcess(cmd)
	cmd.Env = append(os.Environ(), punktfunkConfigDirEnv+"="+b.punktfunkConfigDir())
	out, err := cmd.Output()
	if err != nil {
		log.Printf("[punktfunk] list-monitors failed: %v", err)
		return nil
	}
	return parsePunktfunkMonitors(string(out))
}

// parsePunktfunkMonitors reads `punktfunk-host list-monitors`, whose lines
// (devtest.rs's list_monitors) look like
//
//	Kwin:
//	  HDMI-A-1      3840x2160@60 at +0,+0  scale 2.7  Some Vendor Model  [primary]
//
// Disabled monitors and Punktfunk's own virtual displays are left out:
// neither is a monitor to pin capture to.
func parsePunktfunkMonitors(out string) []CaptureDevice {
	var devices []CaptureDevice
	for _, line := range strings.Split(out, "\n") {
		if !strings.HasPrefix(line, "  ") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		d := CaptureDevice{Key: fields[0], OutputName: fields[0], DisplayName: fields[0]}
		if _, err := fmt.Sscanf(fields[1], "%dx%d", &d.Width, &d.Height); err != nil {
			continue
		}
		rest := strings.TrimSpace(line)
		if i := strings.LastIndex(rest, "  ["); i >= 0 && strings.HasSuffix(rest, "]") {
			tags := rest[i+3 : len(rest)-1]
			rest = rest[:i]
			if strings.Contains(tags, "disabled") || strings.Contains(tags, "punktfunk virtual display") {
				continue
			}
			d.Primary = strings.Contains(tags, "primary")
		}
		// The description follows "scale <n>".
		if i := strings.Index(rest, "  scale "); i >= 0 {
			if desc := strings.Fields(rest[i+len("  scale "):]); len(desc) > 1 {
				d.DisplayName = strings.Join(desc[1:], " ")
			}
		}
		devices = append(devices, d)
	}
	return devices
}
