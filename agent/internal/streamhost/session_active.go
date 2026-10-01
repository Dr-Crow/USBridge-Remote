package streamhost

import (
	"bytes"
	"io"
	"os"
	"strings"
	"sync"
)

// logTailLines reads the last readSize bytes of logFile (or the whole
// file if smaller) and splits them into lines. Shared by CurrentVideoCodec
// (sunshine_codec.go) and sessionActiveFromLog below -- both need the same
// "recent tail of the log, as lines" starting point.
func logTailLines(logFile string, readSize int64) []string {
	if logFile == "" {
		return nil
	}
	f, err := os.Open(logFile)
	if err != nil {
		return nil
	}
	defer f.Close()

	stat, err := f.Stat()
	if err != nil {
		return nil
	}
	size := stat.Size()
	if size == 0 {
		return nil
	}
	if size < readSize {
		readSize = size
	}
	if _, err := f.Seek(-readSize, io.SeekEnd); err != nil {
		return nil
	}
	buf := make([]byte, readSize)
	n, _ := f.Read(buf)
	return strings.Split(string(buf[:n]), "\n")
}

// sessionActiveFromLog reports whether logFile's most recent session
// marker (see sessionStartMarkers/sessionEndMarker in sunshine_codec.go)
// indicates a streaming session is active right now. Sunshine logs these
// lines itself, so this is the only signal available for that backend.
// RustShine (gamestream-server) used to emit matching lines too, but a
// real build's binary no longer contains those literals at all (confirmed
// via `strings`) -- rustshineBackend.SessionActive reads gamestream-
// server's own /api/status instead (see rustshine_codec.go), which is both
// correct and simpler than scraping its log. Scanning backward from EOF
// and stopping at the first marker of either kind means a stale start
// marker from a session that already ended can never be mistaken for an
// active one.
func sessionActiveFromLog(logFile string) bool {
	// Same 32KB window as CurrentVideoCodec: comfortably spans one
	// session's connect sequence while staying bounded.
	lines := logTailLines(logFile, 32*1024)
	for i := len(lines) - 1; i >= 0; i-- {
		if strings.Contains(lines[i], sessionEndMarker) {
			return false
		}
		for _, marker := range sessionStartMarkers {
			if strings.Contains(lines[i], marker) {
				return true
			}
		}
	}
	return false
}

// sessionActiveTracker incrementally tracks whether a streaming session is
// active, instead of repeatedly rescanning a fixed-size tail window of the
// log file the way sessionActiveFromLog alone does. Sunshine (the only
// backend this still drives -- see rustshineBackend.SessionActive in
// rustshine_codec.go for why RustShine reads its admin API instead) logs
// continuous per-frame telemetry while a session runs, so that bounded
// window fills with a few seconds of telemetry noise and the CLIENT
// CONNECTED marker that opened an ongoing session scrolls out of it within
// well under a minute at observed logging rates -- SessionActive() would
// then read false mid-stream for any session that outlives that window.
// Tracking an offset and only scanning newly-appended bytes on each call
// avoids ever losing the marker, and is cheaper besides.
type sessionActiveTracker struct {
	mu     sync.Mutex
	path   string
	offset int64
	active bool
	seeded bool
}

// update reflects logFile's content appended since the previous call into
// the tracker's active state and returns it.
func (t *sessionActiveTracker) update(logFile string) bool {
	t.mu.Lock()
	defer t.mu.Unlock()

	if logFile != t.path {
		t.path = logFile
		t.seeded = false
	}
	if logFile == "" {
		t.active = false
		return false
	}

	stat, err := os.Stat(logFile)
	if err != nil {
		return t.active
	}
	size := stat.Size()

	if !t.seeded || size < t.offset {
		// First call for this path, or the log was truncated/rotated out
		// from under us (see rustshineBackend's oversized-log truncation):
		// reseed from the same bounded tail scan sessionActiveFromLog does
		// standalone, rather than replaying the whole file from byte 0.
		t.active = sessionActiveFromLog(logFile)
		t.offset = size
		t.seeded = true
		return t.active
	}
	if size == t.offset {
		return t.active
	}

	f, err := os.Open(logFile)
	if err != nil {
		return t.active
	}
	defer f.Close()
	if _, err := f.Seek(t.offset, io.SeekStart); err != nil {
		return t.active
	}
	buf := make([]byte, size-t.offset)
	n, _ := io.ReadFull(f, buf)
	data := buf[:n]

	// Only advance past complete lines -- a read landing mid-line (the
	// writer flushed only part of it) must leave that partial line for the
	// next call rather than risk splitting a marker across two scans.
	lastNL := bytes.LastIndexByte(data, '\n')
	if lastNL < 0 {
		return t.active
	}
	complete := data[:lastNL+1]
	t.offset += int64(len(complete))

	for _, line := range strings.Split(string(complete), "\n") {
		if strings.Contains(line, sessionEndMarker) {
			t.active = false
			continue
		}
		for _, marker := range sessionStartMarkers {
			if strings.Contains(line, marker) {
				t.active = true
				break
			}
		}
	}
	return t.active
}

// SessionActive reports whether a Moonlight client is currently mid-stream
// (as opposed to merely paired, or the streamer process merely running
// idle). Prefers the itsme228/Sunshine fork's /api/session-status admin
// route (fetchSunshineSessionStatus, sunshine_pairing.go) -- the app's own
// live rtsp_stream::session_count(), not a log scrape -- and only falls
// back to sessionActiveTracker's log-based scan when that route isn't
// reachable (an already-staged Sunshine build from before this route
// existed, or the admin API genuinely isn't up yet).
func (b *sunshineBackend) SessionActive() bool {
	b.mu.Lock()
	adminPort, logFile := b.adminPort, b.logPath
	b.mu.Unlock()
	if adminPort > 0 {
		if active, ok := fetchSunshineSessionStatus(adminPort, b.AdminUser(), b.adminPass()); ok {
			return active
		}
	}
	return b.sessionTracker.update(logFile)
}
