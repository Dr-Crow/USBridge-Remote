package streamhost

import (
	"fmt"
	"log"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// CaptureAccess is implemented by a backend that captures through the
// desktop compositor rather than KMS, where "may it capture" is the
// compositor's own decision about that one executable -- not a file
// capability the agent can check. Only Punktfunk has it; for Sunshine and
// RustShine the app keeps asking permissions.Service about KMS.
type CaptureAccess interface {
	// CaptureGranted reports whether the streamer can capture the screen
	// right now.
	CaptureGranted() bool
	// RequestCapture does what it takes to get there, and reports why not
	// when it can't.
	RequestCapture() error
}

var _ CaptureAccess = (*punktfunkBackend)(nil)

// punktfunkDesktopFile is the name punktfunk-host's own error message tells
// the user to install, and the one its packages ship for /usr/bin.
const punktfunkDesktopFile = "io.unom.Punktfunk.Host.desktop"

// punktfunkCaptureCacheTTL keeps CaptureGranted from running punktfunk-host
// on every GUI refresh (every 2s, on the GUI's own goroutine).
const punktfunkCaptureCacheTTL = 10 * time.Second

var punktfunkCaptureCache struct {
	sync.Mutex
	bin     string
	at      time.Time
	granted bool
}

// CaptureGranted asks punktfunk-host itself (`probe-compositor`: "exit 0
// iff the compositor is up + ready"). Confirmed live on KDE: it exits 1
// with "KWin does not expose zkde_screencast_unstable_v1 to this client"
// for a binary KWin hasn't authorized, and 0 once it has.
func (b *punktfunkBackend) CaptureGranted() bool {
	bin := b.binaryPath()
	if bin == "" {
		return false
	}
	c := &punktfunkCaptureCache
	c.Lock()
	defer c.Unlock()
	if c.bin == bin && time.Since(c.at) < punktfunkCaptureCacheTTL {
		return c.granted
	}
	cmd := exec.Command(bin, "probe-compositor")
	cmd.Env = append(os.Environ(), punktfunkConfigDirEnv+"="+b.punktfunkConfigDir())
	c.bin, c.at, c.granted = bin, time.Now(), cmd.Run() == nil
	return c.granted
}

// RequestCapture authorizes this punktfunk-host with KWin: KWin hands its
// screencast and fake-input interfaces only to a client whose executable is
// the Exec= of an installed .desktop file listing them in
// X-KDE-Wayland-Interfaces. Punktfunk's packages install one for
// /usr/bin/punktfunk-host; a binary anywhere else needs its own, which a
// per-user file under ~/.local/share/applications satisfies (confirmed
// live, no re-login needed for a path KWin hasn't seen yet). Other
// compositors need no such file, so there is nothing to do for them here.
func (b *punktfunkBackend) RequestCapture() error {
	if runtime.GOOS != "linux" {
		return nil
	}
	bin := b.binaryPath()
	if bin == "" {
		return fmt.Errorf("punktfunk-host is not installed")
	}
	// KWin compares against /proc/<pid>/exe, i.e. the path with every
	// symlink resolved.
	real, err := filepath.EvalSymlinks(bin)
	if err != nil {
		return err
	}
	home, err := os.UserHomeDir()
	if err != nil {
		return err
	}
	dir := filepath.Join(home, ".local", "share", "applications")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	if err := os.WriteFile(filepath.Join(dir, punktfunkDesktopFile), []byte(punktfunkDesktopEntry(real)), 0o644); err != nil {
		return err
	}
	// KWin reads .desktop files through KDE's service cache; without a
	// rebuild it keeps answering from the old one for a while.
	for _, tool := range []string{"kbuildsycoca6", "kbuildsycoca5"} {
		if path, err := exec.LookPath(tool); err == nil {
			if out, err := exec.Command(path).CombinedOutput(); err != nil {
				log.Printf("[punktfunk] %s: %v: %s", tool, err, out)
			}
			break
		}
	}
	punktfunkCaptureCache.Lock()
	punktfunkCaptureCache.at = time.Time{}
	punktfunkCaptureCache.Unlock()
	if !b.CaptureGranted() {
		return fmt.Errorf("the compositor still refuses %s -- log out and back in once (KWin remembers its first answer for an executable until then)", real)
	}
	return nil
}

// ensureCapture does RequestCapture's work before punktfunk-host starts, so
// picking Punktfunk needs no Grant click: the file goes into the user's own
// home, no password involved. Only on a KDE session -- no other compositor
// reads it. It is never removed when another streamer is picked: it names
// one executable and stays hidden, and KWin keeps its first answer for an
// executable until the next login, so taking it away and putting it back
// could leave capture refused until then.
func (b *punktfunkBackend) ensureCapture() {
	if runtime.GOOS != "linux" || !kdeSession() || b.CaptureGranted() {
		return
	}
	if err := b.RequestCapture(); err != nil {
		log.Printf("[punktfunk] screen capture authorization: %v", err)
		return
	}
	log.Printf("[punktfunk] installed %s so KWin lets punktfunk-host capture", punktfunkDesktopFile)
}

// kdeSession reports whether the agent runs inside a KDE Plasma session.
func kdeSession() bool {
	if os.Getenv("KDE_FULL_SESSION") != "" {
		return true
	}
	for _, d := range strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":") {
		if strings.EqualFold(d, "KDE") {
			return true
		}
	}
	return false
}

func punktfunkDesktopEntry(exe string) string {
	return "[Desktop Entry]\n" +
		"Type=Application\n" +
		"Name=Punktfunk Host\n" +
		"Comment=Lets KWin authorize the punktfunk streaming host (installed by USBridge Agent)\n" +
		"Exec=" + exe + "\n" +
		"Terminal=false\n" +
		"NoDisplay=true\n" +
		"X-KDE-Wayland-Interfaces=zkde_screencast_unstable_v1,org_kde_kwin_fake_input,org_kde_plasma_window_management\n"
}
