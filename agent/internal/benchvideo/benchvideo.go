// Package benchvideo plays the streamer benchmark's test content on the
// host: a fast-cut open-movie trailer (Blender's "Sintel" trailer, CC-BY)
// fullscreen and looping, with a small white marker sliding along the
// bottom edge at 60fps on top of it.
//
// The marker is what makes the benchmark measure the streamer rather than
// the content: the trailer itself is 24fps, so without it the screen only
// changes 24 times a second and a capture path that only wakes on real
// damage (DXGI Desktop Duplication, see rust-shine's scripts/
// bench_animator.ps1) would legitimately send fewer frames. With it, every
// display refresh up to 60Hz is a genuinely new picture on both streamers,
// so every missing frame on the client is a real stall.
//
// The client's benchmark (client/internal/gui/benchmark_runner.go) starts
// the content only after its stream has shown the first frame, so the
// streamers' different startup times never shift which part of the trailer
// each one is measured on.
package benchvideo

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"sync"
	"time"
)

const (
	// ContentURL is the Sintel trailer (Blender Foundation, CC-BY 3.0):
	// 52s of fast cuts, fire, snow and camera shake -- a game-trailer-like
	// load for an encoder -- and only ~15MB, so the first run doesn't
	// stall on a download.
	ContentURL  = "https://download.blender.org/durian/trailer/sintel_trailer-1080p.mp4"
	contentName = "sintel_trailer-1080p.mp4"
	// contentMinBytes rejects an HTML error page saved as the video.
	contentMinBytes = 1 << 20

	// maxPlayTime stops the player on its own if the client that started
	// it vanishes mid-benchmark, so a fullscreen video never stays on the
	// host's screen indefinitely.
	maxPlayTime = 15 * time.Minute

	windowTitle = "USBridge Benchmark"
)

// markerFilter overlays a 64px white square that crosses the frame every
// ~1.3s, re-timed to 60fps so every output frame differs from the last.
const markerFilter = "fps=60[v];color=white:s=64x64:r=60[c];[v][c]overlay=x='mod(t*1440\\,W-w)':y=H-h-24:shortest=1"

// Info describes the player state reported to the client.
type Info struct {
	Player      string    `json:"player"`
	Content     string    `json:"content"`
	Playing     bool      `json:"playing"`
	StartedAt   time.Time `json:"started_at,omitempty"`
	ContentPath string    `json:"content_path,omitempty"`
}

// process is a started player: an *exec.Cmd, or on Windows possibly a
// process launched into the interactive console session.
type process interface {
	Kill() error
	Wait() error
}

// Player owns at most one running player process.
type Player struct {
	dir string

	mu       sync.Mutex
	proc     process
	info     Info
	stopTime *time.Timer
	dlMu     sync.Mutex
}

// New returns a Player caching its content under stateDir/bench.
func New(stateDir string) *Player {
	return &Player{dir: filepath.Join(stateDir, "bench")}
}

// Status reports whether a player is running and which one.
func (p *Player) Status() Info {
	p.mu.Lock()
	defer p.mu.Unlock()
	info := p.info
	info.Playing = p.proc != nil
	if info.Player == "" {
		info.Player, _ = findPlayer()
	}
	if info.ContentPath == "" {
		if path, ok := p.cachedContent(); ok {
			info.ContentPath = path
		}
	}
	return info
}

// Prepare makes sure the trailer is cached locally, downloading it once.
// USBRIDGE_BENCH_VIDEO points at a local file to use instead.
func (p *Player) Prepare(ctx context.Context) (string, error) {
	if path := os.Getenv("USBRIDGE_BENCH_VIDEO"); path != "" {
		if _, err := os.Stat(path); err != nil {
			return "", fmt.Errorf("USBRIDGE_BENCH_VIDEO: %w", err)
		}
		return path, nil
	}
	p.dlMu.Lock()
	defer p.dlMu.Unlock()
	if path, ok := p.cachedContent(); ok {
		return path, nil
	}
	if err := os.MkdirAll(p.dir, 0o755); err != nil {
		return "", err
	}
	dst := filepath.Join(p.dir, contentName)
	tmp := dst + ".part"
	log.Printf("[bench] downloading benchmark content %s", ContentURL)
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, ContentURL, nil)
	if err != nil {
		return "", err
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", fmt.Errorf("download benchmark content: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return "", fmt.Errorf("download benchmark content: HTTP %d", resp.StatusCode)
	}
	f, err := os.Create(tmp)
	if err != nil {
		return "", err
	}
	n, err := io.Copy(f, resp.Body)
	if cerr := f.Close(); err == nil {
		err = cerr
	}
	if err == nil && n < contentMinBytes {
		err = fmt.Errorf("download benchmark content: only %d bytes", n)
	}
	if err != nil {
		_ = os.Remove(tmp)
		return "", err
	}
	if err := os.Rename(tmp, dst); err != nil {
		return "", err
	}
	log.Printf("[bench] benchmark content cached at %s (%d bytes)", dst, n)
	return dst, nil
}

func (p *Player) cachedContent() (string, bool) {
	if path := os.Getenv("USBRIDGE_BENCH_VIDEO"); path != "" {
		return path, true
	}
	dst := filepath.Join(p.dir, contentName)
	if st, err := os.Stat(dst); err == nil && st.Size() >= contentMinBytes {
		return dst, true
	}
	return "", false
}

// Start (re)starts the content from its first frame. If the trailer can't
// be fetched it falls back to ffplay's built-in testsrc2 pattern (still
// full-motion 60fps), which only ffplay can generate.
func (p *Player) Start(ctx context.Context) (Info, error) {
	p.Stop()

	content, prepErr := p.Prepare(ctx)
	if prepErr != nil {
		log.Printf("[bench] %v -- falling back to a generated test pattern", prepErr)
	}
	player, path := findPlayer()
	if player == "" {
		return Info{}, errors.New("no video player on the host: install ffmpeg (ffplay), mpv or VLC")
	}
	args, label, err := playerArgs(player, content)
	if err != nil {
		return Info{}, err
	}
	proc, err := launch(path, args)
	if err != nil {
		return Info{}, fmt.Errorf("start %s: %w", player, err)
	}

	exited := make(chan error, 1)
	go func() { exited <- proc.Wait() }()
	// A player that can't open a display exits right away; report that
	// instead of letting the client measure an empty desktop.
	select {
	case err := <-exited:
		return Info{}, fmt.Errorf("%s exited immediately: %v", player, err)
	case <-time.After(1500 * time.Millisecond):
	}

	info := Info{Player: player, Content: label, Playing: true, StartedAt: time.Now(), ContentPath: content}
	p.mu.Lock()
	p.proc = proc
	p.info = info
	p.stopTime = time.AfterFunc(maxPlayTime, func() {
		log.Printf("[bench] benchmark video ran %v without a stop -- stopping it", maxPlayTime)
		p.Stop()
	})
	p.mu.Unlock()
	go func() {
		<-exited
		p.mu.Lock()
		if p.proc == proc {
			p.proc = nil
			p.info.Playing = false
		}
		p.mu.Unlock()
	}()
	log.Printf("[bench] benchmark video started (%s, %s)", player, label)
	return info, nil
}

// Stop closes the player if one is running.
func (p *Player) Stop() {
	p.mu.Lock()
	proc := p.proc
	p.proc = nil
	p.info.Playing = false
	if p.stopTime != nil {
		p.stopTime.Stop()
		p.stopTime = nil
	}
	p.mu.Unlock()
	if proc != nil {
		_ = proc.Kill()
		log.Printf("[bench] benchmark video stopped")
	}
}

func playerArgs(player, content string) (args []string, label string, err error) {
	switch player {
	case "ffplay":
		args = []string{"-hide_banner", "-loglevel", "error", "-fs", "-an", "-alwaysontop", "-window_title", windowTitle}
		if content != "" {
			return append(args, "-loop", "0", "-vf", markerFilter, content), filepath.Base(content), nil
		}
		return append(args, "-f", "lavfi", "-i", "testsrc2=size=1920x1080:rate=60"), "testsrc2 1080p60", nil
	case "mpv":
		if content == "" {
			return nil, "", errors.New("benchmark content unavailable and mpv can't generate a test pattern (install ffmpeg)")
		}
		return []string{"--fs", "--ontop", "--no-audio", "--loop-file=inf", "--no-osc", "--title=" + windowTitle, content}, filepath.Base(content), nil
	case "vlc":
		if content == "" {
			return nil, "", errors.New("benchmark content unavailable and VLC can't generate a test pattern (install ffmpeg)")
		}
		return []string{"--fullscreen", "--video-on-top", "--no-audio", "--loop", "--no-video-title-show", "--no-osd", content}, filepath.Base(content), nil
	}
	return nil, "", fmt.Errorf("unsupported player %q", player)
}

// findPlayer returns the first usable player: ffplay (the only one that
// can overlay the 60fps marker), then mpv, then VLC.
func findPlayer() (name, path string) {
	for _, name := range []string{"ffplay", "mpv", "vlc"} {
		if path := lookPlayer(name); path != "" {
			return name, path
		}
	}
	return "", ""
}

func lookPlayer(name string) string {
	if p, err := exec.LookPath(name); err == nil {
		return p
	}
	// launchd and Windows services start with a minimal PATH.
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = []string{"/opt/homebrew/bin/" + name, "/usr/local/bin/" + name}
		if name == "vlc" {
			candidates = append(candidates, "/Applications/VLC.app/Contents/MacOS/VLC")
		}
	case "windows":
		exe := name + ".exe"
		if la := os.Getenv("LOCALAPPDATA"); la != "" {
			if m, _ := filepath.Glob(filepath.Join(la, "Microsoft", "WinGet", "Packages", "*FFmpeg*", "*", "bin", exe)); len(m) > 0 {
				candidates = append(candidates, m...)
			}
			candidates = append(candidates, filepath.Join(la, "Microsoft", "WinGet", "Links", exe))
		}
		// The service runs as LocalSystem, whose LOCALAPPDATA is not the
		// user's -- winget installs land in the user's profile.
		if m, _ := filepath.Glob(filepath.Join(`C:\Users`, "*", "AppData", "Local", "Microsoft", "WinGet", "Packages", "*FFmpeg*", "*", "bin", exe)); len(m) > 0 {
			candidates = append(candidates, m...)
		}
		for _, pf := range []string{os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)")} {
			if pf == "" {
				continue
			}
			candidates = append(candidates,
				filepath.Join(pf, "ffmpeg", "bin", exe),
				filepath.Join(pf, "mpv", exe),
				filepath.Join(pf, "VideoLAN", "VLC", exe))
		}
		candidates = append(candidates, filepath.Join(`C:\ffmpeg\bin`, exe))
	default:
		candidates = []string{"/usr/local/bin/" + name, "/snap/bin/" + name}
	}
	for _, c := range candidates {
		if st, err := os.Stat(c); err == nil && !st.IsDir() {
			return c
		}
	}
	return ""
}
