// SPDX-License-Identifier: GPL-3.0-only
// CI-only generated-media acceptance. Never use this runner for desktop capture.
package main

import (
	"bytes"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/binary"
	"encoding/hex"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

const sourceCommit = "2e07af3484369bc68bc8091d5a04969867eff0f9"
const sourceArchiveSHA = "7a8ec9b04f0b3f090de55dc6fc7194e7bd7caf93a08a11182c7c03dcfce1ab2b"
const viewerTitle = "Source preview (experimental, this computer)"
const maxProtocolLine = 4096

var hashPattern = regexp.MustCompile(`^[0-9a-f]{64}$`)
var commitPattern = regexp.MustCompile(`^[0-9a-f]{40}$`)
var drivePath = regexp.MustCompile(`^[A-Za-z]:[\\/]`)

// Errors and receipts contain fixed codes only, never paths, child text or keys.
func failure(code string) error { return errors.New(code) }

// An OS exit code cannot prove natural cleanup: kill-on-job-close may report
// zero. The known safety action is authoritative regardless of the exit value.
func naturalChildExit(exitCode uint32, safetyClosed bool) error {
	if safetyClosed {
		return failure("safety_job_closed")
	}
	if exitCode != 0 {
		return failure("child_nonzero_exit")
	}
	return nil
}

type config struct {
	Agent, AgentSHA, Viewer, ViewerSHA, Fixture, FixtureSHA string
	Components, ManifestSHA, SourceSHA, FixtureManifestSHA  string
	Work, Output, Commit                                    string
}
type caseReceipt struct {
	Name             string `json:"name"`
	Passed           bool   `json:"passed"`
	FirstFrame       bool   `json:"typed_first_frame"`
	Blue             bool   `json:"owned_window_blue"`
	Orange           bool   `json:"owned_window_orange"`
	ColorTransitions int    `json:"owned_window_color_transitions"`
	PixelSamples     int    `json:"owned_window_samples"`
	OwnedWindows     int    `json:"owned_top_level_windows_max"`
	VisibleWindows   int    `json:"owned_visible_windows_max"`
	TitleMatches     int    `json:"owned_title_matches_max"`
	VideoFrames      uint64 `json:"source_video_frames"`
	AudioPackets     uint64 `json:"source_audio_packets"`
	FixtureProcesses int    `json:"max_fixture_processes"`
	NaturalCleanup   bool   `json:"natural_cleanup"`
	SafetyKillUsed   bool   `json:"safety_job_kill_used"`
	JobEmpty         bool   `json:"job_empty_before_safety_close"`
	WindowGone       bool   `json:"owned_window_removed"`
	ListenersClosed  bool   `json:"advertised_listeners_closed"`
}
type receipt struct {
	Schema               int           `json:"schema_version"`
	Passed               bool          `json:"passed"`
	Failure              string        `json:"failure_code,omitempty"`
	Platform             string        `json:"platform"`
	Commit               string        `json:"commit,omitempty"`
	SourceCommit         string        `json:"source_streamer_commit"`
	SourceArchive        string        `json:"source_archive_sha256"`
	Role                 string        `json:"encoder_role"`
	AgentSHA             string        `json:"agent_sha256,omitempty"`
	ViewerSHA            string        `json:"viewer_sha256,omitempty"`
	FixtureSHA           string        `json:"fixture_sha256,omitempty"`
	SourceSHA            string        `json:"source_sha256,omitempty"`
	ManifestSHA          string        `json:"manifest_sha256,omitempty"`
	FixtureManifestSHA   string        `json:"fixture_manifest_sha256,omitempty"`
	ActualAgentCLI       bool          `json:"actual_agent_cli_supervision"`
	ActualAgentAppNew    bool          `json:"actual_agent_app_new"`
	ActualMedia          bool          `json:"actual_media_tested"`
	ActualPixels         bool          `json:"actual_owned_window_pixels_tested"`
	PhysicalPresentation bool          `json:"physical_display_presentation_tested"`
	DesktopCapture       bool          `json:"desktop_capture_tested"`
	InputInjection       bool          `json:"input_injection_tested"`
	ManagerEnabled       bool          `json:"windows_agent_preview_manager_enabled"`
	SourceChanged        bool          `json:"source_snapshot_changed"`
	Published            bool          `json:"source_or_binary_artifacts_published"`
	PixelsSaved          bool          `json:"pixels_saved"`
	SystemOnlyPath       bool          `json:"system_only_path"`
	JobContainment       bool          `json:"job_containment"`
	FreshRestart         bool          `json:"fresh_restart_verified"`
	Cases                []caseReceipt `json:"cases"`
}

func baseReceipt(c config) receipt {
	r := receipt{Schema: 1, Platform: "windows/amd64", SourceCommit: sourceCommit, SourceArchive: sourceArchiveSHA, Role: "synthetic-substitution-only", Cases: []caseReceipt{}}
	if commitPattern.MatchString(c.Commit) {
		r.Commit = c.Commit
	}
	for _, p := range []struct {
		v   string
		dst *string
	}{{c.AgentSHA, &r.AgentSHA}, {c.ViewerSHA, &r.ViewerSHA}, {c.FixtureSHA, &r.FixtureSHA}, {c.SourceSHA, &r.SourceSHA}, {c.ManifestSHA, &r.ManifestSHA}, {c.FixtureManifestSHA, &r.FixtureManifestSHA}} {
		if hashPattern.MatchString(p.v) {
			*p.dst = p.v
		}
	}
	return r
}
func main() {
	var c config
	f := flag.NewFlagSet("windows-preview-acceptance", flag.ContinueOnError)
	f.SetOutput(io.Discard)
	for _, p := range []struct {
		n string
		v *string
	}{{"agent", &c.Agent}, {"agent-sha256", &c.AgentSHA}, {"viewer", &c.Viewer}, {"viewer-sha256", &c.ViewerSHA}, {"fixture", &c.Fixture}, {"fixture-sha256", &c.FixtureSHA}, {"components", &c.Components}, {"manifest-sha256", &c.ManifestSHA}, {"source-sha256", &c.SourceSHA}, {"fixture-manifest-sha256", &c.FixtureManifestSHA}, {"work", &c.Work}, {"output", &c.Output}, {"commit", &c.Commit}} {
		f.StringVar(p.v, p.n, "", p.n)
	}
	plan := f.Bool("plan", false, "print the bounded, non-executing test plan")
	if f.Parse(os.Args[1:]) != nil || f.NArg() != 0 {
		fmt.Fprintln(os.Stderr, "invalid_arguments")
		os.Exit(2)
	}
	if *plan {
		_ = json.NewEncoder(os.Stdout).Encode(testPlan())
		return
	}
	r := baseReceipt(c)
	if err := validateConfig(c); err != nil {
		r.Failure = err.Error()
	} else if err = execute(c, &r); err != nil {
		r.Failure = err.Error()
	} else {
		r.Passed = true
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil {
		os.Exit(2)
	}
	raw = append(raw, '\n')
	// This is the only persisted output. The owner supplies a new receipt path.
	if c.Output == "" || writeReceipt(c.Output, raw) != nil {
		fmt.Fprintln(os.Stderr, "receipt_write_failed")
		os.Exit(2)
	}
	_, _ = os.Stdout.Write(raw)
	if !r.Passed {
		os.Exit(1)
	}
}
func testPlan() any {
	return map[string]any{"schema_version": 1, "native_required": "windows/amd64", "cases": []string{"window_close", "stdin_eof", "stdin_extra"}, "encoder_role": "synthetic-substitution-only", "pixel_probe": "PrintWindow into owned memory DIB only", "child_launch": "atomically create in kill-on-close job, verify, then resume", "source_supervision": "actual agent --source-streamer-mode", "desktop_capture_tested": false, "windows_agent_preview_manager_enabled": false}
}
func validateConfig(c config) error {
	for _, h := range []string{c.AgentSHA, c.ViewerSHA, c.FixtureSHA, c.SourceSHA, c.ManifestSHA, c.FixtureManifestSHA} {
		if !hashPattern.MatchString(h) {
			return failure("invalid_hash_pin")
		}
	}
	if !commitPattern.MatchString(c.Commit) {
		return failure("invalid_commit")
	}
	for _, p := range []string{c.Agent, c.Viewer, c.Fixture, c.Components, c.Work, c.Output} {
		if !drivePath.MatchString(p) || strings.ContainsAny(p, "\x00\r\n") || strings.Contains(p[2:], ":") {
			return failure("invalid_local_path")
		}
	}
	for _, p := range []string{c.Agent, c.Viewer, c.Fixture} {
		if !strings.HasSuffix(strings.ToLower(p), ".exe") {
			return failure("invalid_executable_path")
		}
	}
	fixtureName := strings.ReplaceAll(c.Fixture, "\\", "/")
	fixtureName = fixtureName[strings.LastIndex(fixtureName, "/")+1:]
	if !strings.EqualFold(fixtureName, "ffmpeg-fixture.exe") {
		return failure("invalid_fixture_basename")
	}
	if strings.EqualFold(c.Agent, c.Viewer) || strings.EqualFold(c.Agent, c.Fixture) || strings.EqualFold(c.Viewer, c.Fixture) {
		return failure("component_identity_collision")
	}
	return nil
}
func writeReceipt(path string, data []byte) error {
	f, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if err != nil {
		return err
	}
	_, err = f.Write(data)
	closeErr := f.Close()
	if err != nil {
		return err
	}
	return closeErr
}
func fileSHA(f *os.File, limit int64) (string, error) {
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return "", failure("file_read_failed")
	}
	h := sha256.New()
	n, err := io.Copy(h, io.LimitReader(f, limit+1))
	if err != nil || n == 0 || n > limit {
		return "", failure("file_bounds_failed")
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
func exactJSON(raw []byte, target any, keys ...string) error {
	// Reject duplicates, aliases, nulls, trailing data and unknown fields before
	// encoding/json's case-insensitive struct field matching can accept them.
	d := json.NewDecoder(bytes.NewReader(raw))
	tok, e := d.Token()
	if e != nil || tok != json.Delim('{') {
		return failure("invalid_protocol")
	}
	wanted := map[string]bool{}
	for _, k := range keys {
		wanted[k] = true
	}
	for d.More() {
		tok, e = d.Token()
		k, ok := tok.(string)
		if e != nil || !ok || !wanted[k] {
			return failure("invalid_protocol")
		}
		delete(wanted, k)
		var v json.RawMessage
		if d.Decode(&v) != nil || bytes.Equal(bytes.TrimSpace(v), []byte("null")) {
			return failure("invalid_protocol")
		}
	}
	if len(wanted) != 0 {
		return failure("invalid_protocol")
	}
	if tok, e = d.Token(); e != nil || tok != json.Delim('}') {
		return failure("invalid_protocol")
	}
	if _, e = d.Token(); e != io.EOF {
		return failure("invalid_protocol")
	}
	if json.Unmarshal(raw, target) != nil {
		return failure("invalid_protocol")
	}
	return nil
}

type readyEvent struct {
	Schema       int      `json:"schema_version"`
	Event        string   `json:"event"`
	Session      string   `json:"session_id"`
	RTSP         string   `json:"rtsp_address"`
	Control      string   `json:"control_address"`
	Capabilities []string `json:"capabilities"`
}

func parseReady(raw []byte, id string) (readyEvent, error) {
	var r readyEvent
	err := exactJSON(raw, &r, "schema_version", "event", "session_id", "rtsp_address", "control_address", "capabilities")
	if err != nil || r.Schema != 1 || r.Event != "ready" || r.Session != id {
		return r, failure("invalid_source_ready")
	}
	for _, a := range []string{r.RTSP, r.Control} {
		if !loopbackAddress(a) {
			return r, failure("invalid_source_endpoint")
		}
	}
	want := map[string]bool{"rtsp-encrypted": true, "video-windows-gdi-h264": true, "audio-silence": true, "control-enet": true}
	for _, v := range r.Capabilities {
		if !want[v] {
			return r, failure("invalid_source_capability")
		}
		delete(want, v)
	}
	if len(want) != 0 {
		return r, failure("invalid_source_capability")
	}
	return r, nil
}
func loopbackAddress(a string) bool {
	host, port, e := net.SplitHostPort(a)
	if e != nil || host != "127.0.0.1" {
		return false
	}
	n, e := strconv.Atoi(port)
	return e == nil && n > 0 && n <= 65535 && a == fmt.Sprintf("127.0.0.1:%d", n)
}

type stats struct {
	VideoFrames        uint64 `json:"video_frames"`
	VideoPackets       uint64 `json:"video_packets"`
	AudioPackets       uint64 `json:"audio_packets"`
	AudioParityPackets uint64 `json:"audio_parity_packets"`
}

func parseStopped(raw []byte, id string) (stats, error) {
	var r struct {
		Schema  int             `json:"schema_version"`
		Event   string          `json:"event"`
		Session string          `json:"session_id"`
		Reason  string          `json:"reason"`
		Stats   json.RawMessage `json:"stats"`
	}
	var s stats
	if exactJSON(raw, &r, "schema_version", "event", "session_id", "reason", "stats") != nil || r.Schema != 1 || r.Event != "stopped" || r.Session != id || r.Reason != "completed" {
		return s, failure("invalid_source_stopped")
	}
	if exactJSON(r.Stats, &s, "video_frames", "video_packets", "audio_packets", "audio_parity_packets") != nil || s.VideoFrames < 1 || s.VideoPackets < s.VideoFrames || s.AudioPackets < 1 || s.VideoFrames > 900 || s.VideoPackets > 100000 || s.AudioPackets > 6001 {
		return s, failure("invalid_source_media_stats")
	}
	return s, nil
}
func parseViewer(raw []byte, id, kind, reason string) error {
	var e struct {
		Schema  int    `json:"schema_version"`
		Event   string `json:"event"`
		Session string `json:"session_id"`
		Reason  string `json:"reason"`
	}
	keys := []string{"schema_version", "event", "session_id"}
	if kind == "stopped" {
		keys = append(keys, "reason")
	}
	if exactJSON(raw, &e, keys...) != nil || e.Schema != 1 || e.Event != kind || e.Session != id || e.Reason != reason {
		return failure("invalid_viewer_event")
	}
	return nil
}
func privatePayloads(fixture string) (string, map[string]any, map[string]any, error) {
	var secret [36]byte
	if _, err := rand.Read(secret[:]); err != nil {
		return "", nil, nil, failure("random_failed")
	}
	defer clear(secret[:])
	id := hex.EncodeToString(secret[20:])
	key := base64.StdEncoding.EncodeToString(secret[:16])
	keyID := binary.BigEndian.Uint32(secret[16:20])
	source := map[string]any{"schema_version": 1, "owner": "owned-synthetic-ci", "session_id": id, "key_b64": key, "key_id": keyID, "peer_ip": "127.0.0.1", "video_port": 0, "audio_port": 0, "display": "desktop", "capture_consent": true, "ffmpeg": fixture, "width": 128, "height": 72, "fps": 30, "pixel_format": "yuv420p", "packet_size": 1024, "audio_mode": "silence", "max_seconds": 30, "input_consent": false}
	viewer := map[string]any{"schema_version": 1, "profile": "source-preview-v1", "session_id": id, "key_b64": key, "key_id": keyID, "width": 128, "height": 72, "fps": 30, "bitrate_kbps": 10000}
	return id, source, viewer, nil
}
func writePrivate(f *os.File, value any) error {
	raw, e := json.Marshal(value)
	if e != nil {
		return failure("private_payload_failed")
	}
	raw = append(raw, '\n')
	done := make(chan error, 1)
	go func() { defer clear(raw); _, e := f.Write(raw); done <- e }()
	select {
	case e := <-done:
		if e != nil {
			return failure("private_pipe_failed")
		}
		return nil
	case <-time.After(3 * time.Second):
		_ = f.Close()
		return failure("private_pipe_timeout")
	}
}
func childEnvironment(root, work string) []string {
	return []string{"SystemRoot=" + root, "WINDIR=" + root, "PATH=" + filepath.Join(root, "System32") + ";" + root, "TEMP=" + work, "TMP=" + work, "USERPROFILE=" + work, "HOME=" + work, "APPDATA=" + filepath.Join(work, "roaming"), "LOCALAPPDATA=" + filepath.Join(work, "local"), "FYNE_SCALE=1"}
}

// Samples are BGRA bytes from the owned DIB, never read from a screen DC.
func pixelColor(b, g, r byte) int {
	near := func(v byte, w int) bool { return int(v) >= w-28 && int(v) <= w+28 }
	if near(r, 22) && near(g, 75) && near(b, 180) {
		return 1
	}
	if near(r, 219) && near(g, 99) && near(b, 49) {
		return 2
	}
	return 0
}
