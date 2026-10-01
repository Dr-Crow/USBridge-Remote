package streamhost

import (
	"os"
	"path/filepath"
	"testing"
)

func writeTestLog(t *testing.T, content string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "stream.log")
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatalf("write fixture log: %v", err)
	}
	return path
}

func TestSessionActiveFromLog(t *testing.T) {
	cases := []struct {
		name string
		log  string
		want bool
	}{
		{"empty log", "", false},
		{"no session markers at all", "some unrelated line\nanother line\n", false},
		{
			"session started, no disconnect yet",
			`[2026-09-29 18:26:36.700]: Info: New streaming session started [active sessions: 1]
[2026-09-29 18:26:36.730]: Info: CLIENT CONNECTED
[2026-09-29 18:26:40.001]: Info: Client set display cursor: shown`,
			true,
		},
		{
			"session started then disconnected",
			`[2026-09-29 18:26:36.700]: Info: New streaming session started [active sessions: 1]
[2026-09-29 18:26:36.730]: Info: CLIENT CONNECTED
[2026-09-29 18:28:06.022]: Info: CLIENT DISCONNECTED`,
			false,
		},
		{
			"disconnect followed by a later, still-running session",
			`[2026-09-29 18:26:36.700]: Info: New streaming session started [active sessions: 1]
[2026-09-29 18:26:36.730]: Info: CLIENT CONNECTED
[2026-09-29 18:28:06.022]: Info: CLIENT DISCONNECTED
[2026-09-29 18:28:52.991]: Info: New streaming session started [active sessions: 1]
[2026-09-29 18:28:53.021]: Info: CLIENT CONNECTED`,
			true,
		},
		{
			// CLIENT DISCONNECTED right after the newest CLIENT CONNECTED
			// must win, exactly like the real capture in
			// TestSessionActiveFromLog_RealCapture below.
			"connect immediately followed by disconnect",
			`[2026-09-29 18:28:03.644]: Info: New streaming session started [active sessions: 2]
[2026-09-29 18:28:03.665]: Info: CLIENT CONNECTED
[2026-09-29 18:28:06.022]: Info: CLIENT DISCONNECTED`,
			false,
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			path := writeTestLog(t, tc.log)
			if got := sessionActiveFromLog(path); got != tc.want {
				t.Errorf("sessionActiveFromLog() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestSessionActiveFromLog_MissingFile(t *testing.T) {
	if sessionActiveFromLog(filepath.Join(t.TempDir(), "does-not-exist.log")) {
		t.Error("sessionActiveFromLog() on a missing file = true, want false")
	}
	if sessionActiveFromLog("") {
		t.Error(`sessionActiveFromLog("") = true, want false`)
	}
}

// TestSessionActiveFromLog_RealCapture replays a verbatim excerpt from a
// real ~/Library/Application Support/usbridge-agent/logs/sunshine-stdout.log
// capture (RustShine, which logs the same Sunshine-compatible session
// markers) covering three back-to-back sessions, the middle one ending in
// under three seconds -- exactly the "started then immediately
// disconnected" shape sessionActiveFromLog must not misreport as active.
func TestSessionActiveFromLog_RealCapture(t *testing.T) {
	const capture = `[2026-09-29 18:26:27.945]: Info: Client set display cursor: shown
[2026-09-29 18:26:36.700]: Info: New streaming session started [active sessions: 1]
[2026-09-29 18:26:36.730]: Info: CLIENT CONNECTED
[2026-09-29 18:28:02.857]: Warning: SSL Verification error :: Client certificate identity is not enabled
[2026-09-29 18:28:03.554]: Info: Client set display cursor: shown
[2026-09-29 18:28:03.644]: Info: New streaming session started [active sessions: 2]
[2026-09-29 18:28:03.665]: Info: CLIENT CONNECTED
[2026-09-29 18:28:06.022]: Info: CLIENT DISCONNECTED
[2026-09-29 18:28:06.479]: Info: Client set display cursor: shown`
	path := writeTestLog(t, capture)
	if sessionActiveFromLog(path) {
		t.Error("sessionActiveFromLog() = true after the capture's final CLIENT DISCONNECTED, want false")
	}

	const stillStreaming = capture + `
[2026-09-29 18:28:16.328]: Info: Client set display cursor: shown
[2026-09-29 18:28:25.732]: Info: Client set display cursor: shown
[2026-09-29 18:28:52.991]: Info: New streaming session started [active sessions: 1]
[2026-09-29 18:28:53.021]: Info: CLIENT CONNECTED`
	path2 := writeTestLog(t, stillStreaming)
	if !sessionActiveFromLog(path2) {
		t.Error("sessionActiveFromLog() = false after a fresh CLIENT CONNECTED with no following disconnect, want true")
	}
}

// TestSessionActiveTracker_SurvivesMarkerScrollingOutOfTailWindow is the
// regression test for the bug confirmed live on 2026-10-01: a session open
// since 11:31 read as inactive by 15:14 because the log's own per-frame
// telemetry had pushed the CLIENT CONNECTED marker that opened it out of
// sessionActiveFromLog's fixed 32KB tail window, so awdlWatchdog stopped
// reasserting awdl0 down mid-stream. A tracker.update call sequence that
// mirrors real polling (append a little at a time, call update after each
// append) must stay "active" even once total appended noise exceeds 32KB,
// because it only ever scans bytes new since the last call rather than
// rescanning a bounded tail.
func TestSessionActiveTracker_SurvivesMarkerScrollingOutOfTailWindow(t *testing.T) {
	path := writeTestLog(t, "")
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_APPEND, 0o644)
	if err != nil {
		t.Fatalf("open fixture log for append: %v", err)
	}
	defer f.Close()

	appendLine := func(line string) {
		t.Helper()
		if _, err := f.WriteString(line + "\n"); err != nil {
			t.Fatalf("append line: %v", err)
		}
	}

	var tracker sessionActiveTracker

	appendLine("[2026-10-01 11:31:22.910]: Info: New streaming session started [active sessions: 1]")
	appendLine("[2026-10-01 11:31:23.088]: Info: CLIENT CONNECTED")
	if !tracker.update(path) {
		t.Fatal("tracker.update() = false right after CLIENT CONNECTED, want true")
	}

	// Simulate the real capture: ~300 bytes of telemetry noise per line,
	// polled one line at a time, well past the 32KB window the old
	// tail-scan-only approach used.
	noiseLine := "[2026-10-01 15:13:41.804919]: pipeline: frame stage timing sample frame_index=118200 encode_us=10327 packetize_us=49 send_us=381 backlog_drops_since_sample=0 fec_percent=30"
	totalNoise := 0
	for totalNoise < 64*1024 {
		appendLine(noiseLine)
		totalNoise += len(noiseLine) + 1
		if !tracker.update(path) {
			t.Fatalf("tracker.update() = false after %d bytes of post-connect noise, want true (session never disconnected)", totalNoise)
		}
	}

	appendLine("[2026-10-01 15:14:00.000]: Info: CLIENT DISCONNECTED")
	if tracker.update(path) {
		t.Error("tracker.update() = true after CLIENT DISCONNECTED, want false")
	}
}

// TestBackendSessionActive covers sunshineBackend only -- rustshineBackend
// reads /api/status instead of its log (see rustshineBackend.SessionActive
// in rustshine_codec.go and its own test coverage in
// rustshine_backend_test.go).
func TestBackendSessionActive(t *testing.T) {
	logPath := writeTestLog(t, `[2026-09-29 18:26:36.700]: Info: New streaming session started [active sessions: 1]
[2026-09-29 18:26:36.730]: Info: CLIENT CONNECTED`)

	sb := &sunshineBackend{logPath: logPath}
	if !sb.SessionActive() {
		t.Error("sunshineBackend.SessionActive() = false, want true")
	}
}
