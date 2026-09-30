package streamhost

import (
	"io"
	"os"
	"strings"
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
// indicates a streaming session is active right now. Both Sunshine and
// RustShine (gamestream-server) log the same "New streaming session
// started"/"CLIENT CONNECTED"/"CLIENT DISCONNECTED" lines, so this one
// scan works for both backends. Scanning backward from EOF and stopping at
// the first marker of either kind means a stale start marker from a
// session that already ended can never be mistaken for an active one.
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

// SessionActive reports whether a Moonlight client is currently mid-stream
// (as opposed to merely paired, or the streamer process merely running
// idle) -- see sessionActiveFromLog.
func (b *sunshineBackend) SessionActive() bool {
	b.mu.Lock()
	logFile := b.logPath
	b.mu.Unlock()
	return sessionActiveFromLog(logFile)
}

// SessionActive is RustShine's counterpart to sunshineBackend.SessionActive
// -- see sessionActiveFromLog. RustShine's /api/status endpoint (see
// rustshine_codec.go's fetchStatus) doesn't report an active-session flag,
// only the last-negotiated codec/color-mode, so the log-anchored scan is
// used here too rather than a second, backend-specific mechanism.
func (b *rustshineBackend) SessionActive() bool {
	b.mu.Lock()
	logFile := b.logPath
	b.mu.Unlock()
	return sessionActiveFromLog(logFile)
}
