package main

import (
	"bytes"
	"encoding/base64"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func validConfig() config {
	h := strings.Repeat("a", 64)
	return config{Agent: `C:\private\agent.exe`, AgentSHA: h, Viewer: `C:\private\viewer.exe`, ViewerSHA: h, Fixture: `C:\private\ffmpeg-fixture.exe`, FixtureSHA: h, SourceSHA: h, Components: `C:\private\components`, ManifestSHA: h, FixtureManifestSHA: h, Work: `C:\private\new-work`, Output: `C:\receipts\result.json`, Commit: strings.Repeat("b", 40)}
}
func TestConfigRequiresPinnedLocalPaths(t *testing.T) {
	if e := validateConfig(validConfig()); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{`\\server\share\tool.exe`, `C:relative.exe`, `C:\ok.exe:stream`, `C:\bad.exe` + "\n", `/tmp/tool.exe`, `C:\owned\ffmpeg.exe`, ""} {
		c := validConfig()
		c.Fixture = bad
		if validateConfig(c) == nil {
			t.Fatal("unsafe path accepted")
		}
	}
	for _, bad := range []string{"", strings.Repeat("A", 64), strings.Repeat("a", 63), strings.Repeat("g", 64)} {
		c := validConfig()
		c.FixtureSHA = bad
		if validateConfig(c) == nil {
			t.Fatal("untrusted hash accepted")
		}
	}
	c := validConfig()
	c.Fixture = c.Viewer
	if validateConfig(c) == nil {
		t.Fatal("roles share a path")
	}
}
func TestExactJSONRejectsAmbiguity(t *testing.T) {
	var out struct {
		Event string `json:"event"`
	}
	if exactJSON([]byte(`{"event":"ready"}`), &out, "event") != nil {
		t.Fatal("valid object rejected")
	}
	for _, s := range []string{`{"Event":"ready"}`, `{"event":"ready","event":"stopped"}`, `{"event":null}`, `{"event":"ready","key_b64":"must-not-escape"}`, `{}`, `{"event":"ready"}{}`, `[]`} {
		e := exactJSON([]byte(s), &out, "event")
		if e == nil || e.Error() != "invalid_protocol" {
			t.Fatal("ambiguity accepted or input echoed")
		}
	}
}

const session = "0123456789abcdef0123456789abcdef"

func sourceReady() string {
	return `{"schema_version":1,"event":"ready","session_id":"` + session + `","rtsp_address":"127.0.0.1:48010","control_address":"127.0.0.1:48011","capabilities":["rtsp-encrypted","video-windows-gdi-h264","audio-silence","control-enet"]}`
}
func TestReadyEnforcesIdentityLoopbackAndCapabilities(t *testing.T) {
	if _, e := parseReady([]byte(sourceReady()), session); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(sourceReady(), "127.0.0.1:48010", "localhost:48010", 1), strings.Replace(sourceReady(), "127.0.0.1:48010", "127.0.0.1:http", 1), strings.Replace(sourceReady(), "127.0.0.1:48010", "127.0.0.1:048010", 1), strings.Replace(sourceReady(), "video-windows-gdi-h264", "input-x11-keyboard-mouse", 1), strings.Replace(sourceReady(), "control-enet", "audio-silence", 1), strings.Replace(sourceReady(), session, "wrong", 1), strings.Replace(sourceReady(), `"schema_version":1`, `"schema_version":1,"key_b64":"secret"`, 1)} {
		if _, e := parseReady([]byte(bad), session); e == nil {
			t.Fatal("unsafe readiness accepted")
		}
	}
}
func TestViewerRequiresTypedFirstFrameAndOrderedCallerExpectations(t *testing.T) {
	for _, event := range []string{"ready", "first_frame"} {
		raw := []byte(`{"schema_version":1,"event":"` + event + `","session_id":"` + session + `"}`)
		if parseViewer(raw, session, event, "") != nil {
			t.Fatal("valid event rejected")
		}
		if parseViewer(raw, session, "stopped", "completed") == nil {
			t.Fatal("wrong event accepted")
		}
		if parseViewer(append(raw, []byte(`{}`)...), session, event, "") == nil {
			t.Fatal("trailing output accepted")
		}
	}
	raw := []byte(`{"schema_version":1,"event":"stopped","session_id":"` + session + `","reason":"failed"}`)
	if parseViewer(raw, session, "stopped", "failed") != nil || parseViewer(raw, session, "stopped", "completed") == nil {
		t.Fatal("failure reason not enforced")
	}
}
func TestSourceStoppedRequiresRealMediaStats(t *testing.T) {
	raw := `{"schema_version":1,"event":"stopped","session_id":"` + session + `","reason":"completed","stats":{"video_frames":90,"video_packets":90,"audio_packets":600,"audio_parity_packets":0}}`
	if _, e := parseStopped([]byte(raw), session); e != nil {
		t.Fatal(e)
	}
	for _, bad := range []string{strings.Replace(raw, `"video_frames":90`, `"video_frames":0`, 1), strings.Replace(raw, `"audio_packets":600`, `"audio_packets":0`, 1), strings.Replace(raw, `"video_frames":90`, `"video_frames":901`, 1), strings.Replace(raw, `"video_frames":90`, `"video_frames":90,"video_frames":90`, 1), strings.Replace(raw, `"stats":{`, `"stats":null,"extra":{`, 1), strings.Replace(raw, `"completed"`, `"failed"`, 1)} {
		if _, e := parseStopped([]byte(bad), session); e == nil {
			t.Fatal("invalid stats accepted")
		}
	}
}
func TestPrivatePayloadsFreshAndGeneratedOnly(t *testing.T) {
	a, source, viewer, e := privatePayloads(`C:\owned\ffmpeg-fixture.exe`)
	if e != nil {
		t.Fatal(e)
	}
	b, second, _, e := privatePayloads(`C:\owned\ffmpeg-fixture.exe`)
	if e != nil || a == b || source["key_b64"] == second["key_b64"] {
		t.Fatal("session reuse")
	}
	key, e := base64.StdEncoding.Strict().DecodeString(source["key_b64"].(string))
	if e != nil || len(key) != 16 || viewer["key_b64"] != source["key_b64"] || viewer["key_id"] != source["key_id"] {
		t.Fatal("private key contract")
	}
	if source["ffmpeg"] != `C:\owned\ffmpeg-fixture.exe` || source["input_consent"] != false || source["peer_ip"] != "127.0.0.1" || source["max_seconds"] != 30 || source["width"] != 128 || source["height"] != 72 || source["audio_mode"] != "silence" {
		t.Fatal("generated profile drift")
	}
	if _, ok := viewer["rtsp_url"]; ok {
		t.Fatal("endpoint chosen before source readiness")
	}
}
func TestPrivatePayloadOnlyTravelsPipe(t *testing.T) {
	in, out, e := os.Pipe()
	if e != nil {
		t.Fatal(e)
	}
	defer in.Close()
	defer out.Close()
	data := make(chan []byte, 1)
	go func() { raw, _ := io.ReadAll(in); data <- raw }()
	if writePrivate(out, map[string]any{"key_b64": "secret-fixture"}) != nil {
		t.Fatal("write")
	}
	out.Close()
	raw := <-data
	if !bytes.Equal(raw, []byte("{\"key_b64\":\"secret-fixture\"}\n")) {
		t.Fatal("pipe contract")
	}
}
func TestEnvironmentDoesNotInheritDiagnosticsOrCredentials(t *testing.T) {
	t.Setenv("USBRIDGE_FRAME_DUMP_DIR", "forbidden")
	t.Setenv("FFREPORT", "forbidden")
	t.Setenv("TOKEN", "forbidden")
	env := childEnvironment(`C:\Windows`, `C:\private`)
	for _, v := range env {
		if strings.Contains(v, "forbidden") || strings.HasPrefix(v, "USBRIDGE_") || strings.HasPrefix(v, "FFREPORT=") {
			t.Fatal("inherited environment")
		}
	}
	if len(env) != 10 {
		t.Fatal("environment allowlist changed")
	}
	if !strings.Contains(strings.Join(env, "\n"), `PATH=C:\Windows`) {
		t.Fatal("system path missing")
	}
}
func TestPixelRecognitionRequiresOwnedBlueOrange(t *testing.T) {
	for _, p := range []struct {
		b, g, r byte
		want    int
	}{{180, 75, 22, 1}, {49, 99, 219, 2}, {0, 0, 0, 0}, {255, 255, 255, 0}, {0, 255, 0, 0}} {
		if pixelColor(p.b, p.g, p.r) != p.want {
			t.Fatal("pixel match drift")
		}
	}
}
func TestReceiptExcludesPathsKeysAndUnprovenClaims(t *testing.T) {
	c := validConfig()
	c.Commit = "secret-fixture"
	c.AgentSHA = "secret-fixture"
	r := baseReceipt(c)
	raw, e := json.Marshal(r)
	if e != nil {
		t.Fatal(e)
	}
	for _, secret := range []string{"secret-fixture", c.Agent, c.Viewer, c.Fixture, c.Components, c.Work, "key_b64", "session_id", "rtsp_url"} {
		if bytes.Contains(raw, []byte(secret)) {
			t.Fatal("receipt includes private input")
		}
	}
	if r.Passed || r.ActualMedia || r.ActualPixels || r.DesktopCapture || r.InputInjection || r.PhysicalPresentation || r.ActualAgentCLI || r.ActualAgentAppNew || r.ManagerEnabled {
		t.Fatal("unverified claims")
	}
	path := filepath.Join(t.TempDir(), "receipt.json")
	if writeReceipt(path, raw) != nil || writeReceipt(path, raw) == nil {
		t.Fatal("receipt must be exclusive")
	}
}
func TestPlanCannotClaimNativePass(t *testing.T) {
	raw, e := json.Marshal(testPlan())
	if e != nil || bytes.Contains(raw, []byte(`"passed":true`)) {
		t.Fatal("plan claims execution")
	}
}

func TestSafetyClosureRejectsZeroAndNonzeroExit(t *testing.T) {
	for _, code := range []uint32{0, 1, 259, 0xffffffff} {
		err := naturalChildExit(code, true)
		if err == nil || err.Error() != "safety_job_closed" {
			t.Fatal("known safety action accepted as natural completion")
		}
	}
	if naturalChildExit(0, false) != nil {
		t.Fatal("natural zero exit rejected")
	}
	if naturalChildExit(1, false) == nil {
		t.Fatal("natural nonzero exit accepted")
	}
}
