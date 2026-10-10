//go:build linux

package sourcepreview

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"usbridge_agent/internal/localcomponents"
)

// This opt-in fixture runs only on two explicitly owned CI Xvfb displays. It
// executes the real verified viewer and source, then independently inspects
// presented viewer pixels instead of trusting a decoded-image callback.
func TestNativeSourcePreviewRenderer(t *testing.T) {
	if os.Getenv("SOURCE_PREVIEW_NATIVE_ACCEPTANCE") != "1" {
		t.Skip("requires separately provisioned native viewer and isolated Xvfb")
	}
	if os.Getenv("DISPLAY") != ":97" || os.Getenv("SOURCE_PREVIEW_CAPTURE_DISPLAY") != ":96" {
		t.Fatal("wrong acceptance displays")
	}
	root := os.Getenv("SOURCE_PREVIEW_COMPONENTS")
	pin := os.Getenv("SOURCE_PREVIEW_MANIFEST_SHA256")
	probe := os.Getenv("SOURCE_PREVIEW_PIXEL_PROBE")
	out := os.Getenv("SOURCE_PREVIEW_OUTPUT")
	if !filepath.IsAbs(root) || !filepath.IsAbs(probe) || !filepath.IsAbs(out) {
		t.Fatal("absolute fixture paths required")
	}
	pulse, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	pulseContact := make(chan struct{}, 1)
	pulseDone := make(chan struct{})
	go func() {
		defer close(pulseDone)
		conn, e := pulse.Accept()
		if e == nil {
			conn.Close()
			pulseContact <- struct{}{}
		}
	}()
	defer func() { pulse.Close(); <-pulseDone }()
	t.Setenv("PULSE_SERVER", "tcp:"+pulse.Addr().String())
	dump := t.TempDir()
	t.Setenv("USBRIDGE_FRAME_DUMP_DIR", dump)
	t.Setenv("USBRIDGE_FRAME_DUMP_EVERY_N", "1")
	t.Setenv("USBRIDGE_PYROWAVE_DUMP", filepath.Join(dump, "frame.pgm"))
	t.Setenv("USBRIDGE_SKIP_DECODE", "1")
	m := New()
	defer m.Stop()
	a := Approval{Components: localcomponents.Options{Directory: root, ManifestSHA256: pin, StateDir: t.TempDir()}, Display: ":96", FFmpeg: "/usr/bin/ffmpeg", CaptureConsent: true}
	runProbe := func(mode, name string) {
		t.Helper()
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, "/usr/bin/python3", probe, "--mode", mode, "--output", filepath.Join(out, name+".json"))
		if raw, err := cmd.CombinedOutput(); err != nil {
			t.Fatalf("pixel probe %s: %v %s", mode, err, raw)
		}
	}
	ids := map[string]bool{}
	for _, kind := range []string{"viewer-button", "parent-stop", "expiry"} {
		if kind == "expiry" {
			m.deps.now = func() time.Time { return time.Now().Add(-23 * time.Second) }
		}
		s, err := m.Start(context.Background(), a)
		if err != nil {
			t.Fatal(err)
		}
		if ids[s.ID()] {
			t.Fatal("session reused")
		}
		ids[s.ID()] = true
		if kind != "expiry" {
			runProbe("pixels", kind+"-pixels")
		}
		switch kind {
		case "viewer-button":
			runProbe("stop", kind+"-button")
		case "parent-stop":
			if err = m.Stop(); err != nil {
				t.Fatal(err)
			}
		}
		select {
		case <-s.Done():
		case <-time.After(10 * time.Second):
			m.Stop()
			t.Fatal("native preview did not join")
		}
		if err = s.Wait(); err != nil {
			t.Fatal(err)
		}
		runProbe("gone", kind+"-gone")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := m.Start(ctx, a); err == nil {
		t.Fatal("canceled setup succeeded")
	}
	runProbe("gone", "cancel-gone")
	files, err := os.ReadDir(dump)
	if err != nil || len(files) != 0 {
		t.Fatal("diagnostic setting recorded frames")
	}
	select {
	case <-pulseContact:
		t.Fatal("silent preview contacted configured PulseAudio endpoint")
	default:
	}
	receipt := map[string]any{"passed": true, "actual_native_viewer": true, "presented_pixels_verified": true, "viewer_button_joined": true, "parent_stop_joined": true, "expiry_joined": true, "canceled_setup_rejected": true, "fresh_session_ids": true, "inherited_frame_dumps_disabled": true, "configured_pulse_endpoint_not_contacted": true, "capture_display": ":96", "viewer_display": ":97", "width": 128, "height": 72, "audio": "synthetic silence", "input_consent": false, "user_desktop_captured": false}
	raw, _ := json.MarshalIndent(receipt, "", "  ")
	if err = os.WriteFile(filepath.Join(out, "result.json"), append(raw, '\n'), 0600); err != nil {
		t.Fatal(err)
	}
}
