package sourcestreamer

import (
	"bufio"
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"
	"usbridge_agent/internal/localcomponents"
)

func TestMain(m *testing.M) {
	if mode := os.Getenv("USBRIDGE_SOURCE_TEST_HELPER"); mode != "" && len(os.Args) > 1 && os.Args[1] == "--launch-stdin" {
		reader := bufio.NewReader(os.Stdin)
		line, _ := reader.ReadBytes('\n')
		var launch Launch
		if json.Unmarshal(line, &launch) != nil {
			os.Exit(9)
		}
		// A child must not receive the secret request on its process command line.
		for _, arg := range os.Args {
			if strings.Contains(arg, launch.KeyB64) {
				os.Exit(8)
			}
		}
		ready := Ready{1, "ready", launch.SessionID, "127.0.0.1:12341", "127.0.0.1:12342", []string{"rtsp-encrypted", "video-x11-h264", "audio-silence", "control-enet"}}
		if launch.Display == "desktop" {
			ready.Capabilities[1] = "video-windows-gdi-h264"
		}
		if launch.InputConsent {
			ready.Capabilities = append(ready.Capabilities, "input-x11-keyboard-mouse")
		}
		switch mode {
		case "bad-peer":
			ready.ControlAddress = "0.0.0.0:12342"
		case "bad-session":
			ready.SessionID = "another-session"
		case "missing-control":
			ready.Capabilities = ready.Capabilities[:3]
		case "bad-json":
			fmt.Println(`{"key_b64":"never-echo-me"}`)
			io.Copy(io.Discard, reader)
			os.Exit(0)
		case "huge":
			fmt.Println(strings.Repeat("x", MaxMessage+1))
			io.Copy(io.Discard, reader)
			os.Exit(0)
		case "hang":
			io.Copy(io.Discard, reader)
			os.Exit(0)
		}
		json.NewEncoder(os.Stdout).Encode(ready)
		io.Copy(io.Discard, reader)
		json.NewEncoder(os.Stdout).Encode(Stopped{SchemaVersion: 1, Event: "stopped", SessionID: launch.SessionID, Reason: "completed", Stats: &Stats{}})
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func validLaunch() Launch {
	r := Launch{1, "local-owner", "test-session", base64.StdEncoding.EncodeToString([]byte("freshkey-16-byte")), 17, "127.0.0.1", 50001, 50002, ":99", true, filepath.Join(os.TempDir(), "ffmpeg"), 1280, 720, 60, "yuv420p", 1200, "silence", 30, false}
	if runtime.GOOS == "windows" {
		r.Display = "desktop"
		r.FFmpeg += ".exe"
	}
	return r
}
func TestLaunchValidation(t *testing.T) {
	cases := []struct {
		name   string
		mutate func(*Launch)
	}{
		{"schema", func(r *Launch) { r.SchemaVersion = 2 }}, {"identity", func(r *Launch) { r.Owner = "" }},
		{"key", func(r *Launch) { r.KeyB64 = "bad" }}, {"consent", func(r *Launch) { r.CaptureConsent = false }},
		{"remote", func(r *Launch) { r.PeerIP = "192.168.1.2" }}, {"hostname", func(r *Launch) { r.PeerIP = "localhost" }},
		{"port", func(r *Launch) { r.VideoPort = -1 }}, {"ports-same", func(r *Launch) { r.VideoPort = r.AudioPort }},
		{"display", func(r *Launch) { r.Display = "remote:0" }}, {"ffmpeg", func(r *Launch) { r.FFmpeg = "ffmpeg" }},
		{"dimensions", func(r *Launch) { r.Width = 32768 }}, {"fps", func(r *Launch) { r.FPS = 1000 }},
		{"pixel", func(r *Launch) { r.PixelFormat = "rgb" }}, {"packet", func(r *Launch) { r.PacketSize = 65535 }},
		{"audio", func(r *Launch) { r.AudioMode = "native" }}, {"duration", func(r *Launch) { r.MaxSeconds = 301 }},
	}
	for _, tt := range cases {
		t.Run(tt.name, func(t *testing.T) {
			r := validLaunch()
			tt.mutate(&r)
			if r.Validate() == nil {
				t.Fatal("unsafe launch accepted")
			}
		})
	}
	if err := validLaunch().Validate(); err != nil {
		t.Fatal(err)
	}
}
func TestDecodeLaunchStrict(t *testing.T) {
	raw, _ := json.Marshal(validLaunch())
	if _, err := DecodeLaunch(bytes.NewReader(raw)); err != nil {
		t.Fatal(err)
	}
	for _, bad := range [][]byte{append(append([]byte{}, raw...), []byte(" {}")...), bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"schema_version":1,"unknown":true`), 1), bytes.Repeat([]byte("x"), MaxMessage+1)} {
		if _, err := DecodeLaunch(bytes.NewReader(bad)); err == nil {
			t.Fatal("invalid JSON accepted")
		}
	}
}
func TestSupervisorLifecycle(t *testing.T) {
	t.Setenv("USBRIDGE_SOURCE_TEST_HELPER", "valid")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for i := 0; i < 2; i++ {
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		session, err := startBinary(ctx, binary, validLaunch())
		if err != nil {
			cancel()
			t.Fatal(err)
		}
		if session.Ready.SessionID != "test-session" {
			t.Fatal("wrong session")
		}
		if err := session.Stop(); err != nil {
			t.Fatal(err)
		}
		if err := session.Stop(); err != nil {
			t.Fatal("repeated Stop:", err)
		}
		cancel()
	}
}
func TestSupervisorFailsClosed(t *testing.T) {
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	for _, mode := range []string{"bad-peer", "bad-session", "missing-control", "bad-json", "huge", "hang"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("USBRIDGE_SOURCE_TEST_HELPER", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
			defer cancel()
			session, err := startBinary(ctx, binary, validLaunch())
			if err == nil {
				session.Stop()
				t.Fatal("invalid child accepted")
			}
			if strings.Contains(err.Error(), "never-echo-me") {
				t.Fatal("child output leaked")
			}
		})
	}
}
func TestSupervisorNeedsManifestPin(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("source-streamer native runtime unavailable")
	}
	_, err := Start(context.Background(), localcomponents.Options{StateDir: t.TempDir()}, validLaunch())
	if err == nil || !strings.Contains(err.Error(), "pinned") {
		t.Fatal(err)
	}
}

func TestStartVerifiedManifest(t *testing.T) {
	if runtime.GOOS != "linux" && runtime.GOOS != "windows" {
		t.Skip("source-streamer native runtime unavailable")
	}
	t.Setenv("USBRIDGE_SOURCE_TEST_HELPER", "valid")
	binary, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	executable, err := os.ReadFile(binary)
	if err != nil {
		t.Fatal(err)
	}
	directory := t.TempDir()
	name := "source-streamer"
	if runtime.GOOS == "windows" {
		name += ".exe"
	}
	if err := os.WriteFile(filepath.Join(directory, name), executable, 0700); err != nil {
		t.Fatal(err)
	}
	hash := sha256.Sum256(executable)
	manifest := localcomponents.Manifest{Schema: 1, Components: []localcomponents.Component{{Name: "source-streamer", Platform: runtime.GOOS + "/" + runtime.GOARCH, Version: "test-fixture", Profile: Profile, Entry: name, Files: []localcomponents.File{{Path: name, SHA256: hex.EncodeToString(hash[:]), Size: int64(len(executable)), Executable: true}}}}}
	raw, _ := json.Marshal(manifest)
	pin := sha256.Sum256(raw)
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	options := localcomponents.Options{StateDir: t.TempDir(), Directory: directory, ManifestSHA256: hex.EncodeToString(pin[:])}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	session, err := Start(ctx, options, validLaunch())
	if err != nil {
		t.Fatal(err)
	}
	if err := session.Stop(); err != nil {
		t.Fatal(err)
	}
	// A manifest with the wrong semantic profile is never passed to the child.
	manifest.Components[0].Profile = "audited-vendor-v0.3.131"
	raw, _ = json.Marshal(manifest)
	pin = sha256.Sum256(raw)
	options.ManifestSHA256 = hex.EncodeToString(pin[:])
	if err := os.WriteFile(filepath.Join(directory, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if session, err := Start(ctx, options, validLaunch()); err == nil {
		session.Stop()
		t.Fatal("vendor profile accepted as source protocol")
	}
}

func TestDynamicMediaPorts(t *testing.T) {
	r := validLaunch()
	r.VideoPort = 0
	r.AudioPort = 0
	if err := r.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestDecodeLaunchDuplicateKeys(t *testing.T) {
	raw, _ := json.Marshal(validLaunch())
	raw = bytes.Replace(raw, []byte(`"schema_version":1`), []byte(`"schema_version":1,"schema_version":1`), 1)
	if _, err := DecodeLaunch(bytes.NewReader(raw)); err == nil {
		t.Fatal("duplicate accepted")
	}
}
func TestLaunchPacketAlignment(t *testing.T) {
	r := validLaunch()
	r.PacketSize = 1199
	if r.Validate() == nil {
		t.Fatal("unaligned packet size accepted")
	}
}

func TestInputCapabilityRequiresSeparateConsent(t *testing.T) {
	ready := Ready{1, "ready", "session", "127.0.0.1:1000", "127.0.0.1:1001", []string{"rtsp-encrypted", "video-x11-h264", "audio-silence", "control-enet"}}
	if ready.validate("session", true, "video-x11-h264") == nil {
		t.Fatal("missing input capability accepted")
	}
	ready.Capabilities = append(ready.Capabilities, "input-x11-keyboard-mouse")
	if ready.validate("session", false, "video-x11-h264") == nil {
		t.Fatal("input capability accepted without consent")
	}
	if err := ready.validate("session", true, "video-x11-h264"); err != nil {
		t.Fatal(err)
	}
}

func TestWindowsLaunchAndCapabilityBoundary(t *testing.T) {
	r := validLaunch()
	r.Display = "desktop"
	r.FFmpeg = `C:\trusted\ffmpeg.exe`
	r.InputConsent = false
	if err := r.validatePlatform("windows"); err != nil {
		t.Fatal(err)
	}
	for _, mutate := range []func(*Launch){func(r *Launch) { r.Display = ":99" }, func(r *Launch) { r.InputConsent = true }, func(r *Launch) { r.FFmpeg = `\\server\share\ffmpeg.exe` }, func(r *Launch) { r.FFmpeg = `C:ffmpeg.exe` }, func(r *Launch) { r.FFmpeg = `C:\trusted\ffmpeg.bat` }} {
		bad := r
		mutate(&bad)
		if bad.validatePlatform("windows") == nil {
			t.Fatal("invalid Windows launch accepted")
		}
	}
	ready := Ready{1, "ready", "session", "127.0.0.1:1000", "127.0.0.1:1001", []string{"rtsp-encrypted", "video-windows-gdi-h264", "audio-silence", "control-enet"}}
	if err := ready.validate("session", false, "video-windows-gdi-h264"); err != nil {
		t.Fatal(err)
	}
	if ready.validate("session", false, "video-x11-h264") == nil || ready.validate("session", true, "video-windows-gdi-h264") == nil {
		t.Fatal("cross-platform capability accepted")
	}
}
