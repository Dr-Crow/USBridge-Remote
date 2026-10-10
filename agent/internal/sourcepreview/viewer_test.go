package sourcepreview

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"usbridge_agent/internal/localcomponents"
)

func TestMain(m *testing.M) {
	if len(os.Args) > 1 && os.Args[1] == "--source-preview-stdin" {
		var d descriptor
		if json.NewDecoder(os.Stdin).Decode(&d) != nil {
			os.Exit(2)
		}
		event := func(kind, reason string) {
			_ = json.NewEncoder(os.Stdout).Encode(viewerEvent{SchemaVersion: 1, Event: kind, SessionID: d.SessionID, Reason: reason})
		}
		switch os.Getenv("TEST_SOURCE_PREVIEW_EVENT") {
		case "unknown":
			io.WriteString(os.Stdout, "{\"schema_version\":1,\"event\":\"ready\",\"session_id\":\"test\",\"unexpected\":true}\n")
			os.Exit(0)
		case "duplicate":
			io.WriteString(os.Stdout, "{\"schema_version\":1,\"event\":\"ready\",\"event\":\"ready\",\"session_id\":\"test\"}\n")
			os.Exit(0)
		}
		event("ready", "")
		event("first_frame", "")
		_, _ = io.Copy(io.Discard, os.Stdin)
		if os.Getenv("TEST_SOURCE_PREVIEW_EVENT") != "missing-stop" {
			event("stopped", "completed")
		}
		os.Exit(0)
	}
	os.Exit(m.Run())
}
func preparedTestViewer(t *testing.T) string {
	t.Helper()
	exe, e := os.Executable()
	if e != nil {
		t.Fatal(e)
	}
	data, e := os.ReadFile(exe)
	if e != nil {
		t.Fatal(e)
	}
	root := t.TempDir()
	path := filepath.Join(root, "viewer")
	if e = os.WriteFile(path, data, 0700); e != nil {
		t.Fatal(e)
	}
	h := sha256.Sum256(data)
	manifest := localcomponents.Manifest{Schema: 1, Components: []localcomponents.Component{{Name: "source-preview-viewer", Platform: runtime.GOOS + "/" + runtime.GOARCH, Version: "test", Profile: Profile, Entry: "viewer", Files: []localcomponents.File{{Path: "viewer", SHA256: hex.EncodeToString(h[:]), Size: int64(len(data)), Executable: true}}}}}
	raw, _ := json.Marshal(manifest)
	if e = os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0600); e != nil {
		t.Fatal(e)
	}
	pin := sha256.Sum256(raw)
	path, e = prepareViewer(context.Background(), localcomponents.Options{Directory: root, ManifestSHA256: hex.EncodeToString(pin[:]), StateDir: t.TempDir()})
	if e != nil {
		t.Fatal(e)
	}
	return path
}
func TestActualViewerProcessProtocol(t *testing.T) {
	path := preparedTestViewer(t)
	for _, mode := range []string{"", "unknown", "duplicate", "missing-stop"} {
		t.Run(mode, func(t *testing.T) {
			t.Setenv("TEST_SOURCE_PREVIEW_EVENT", mode)
			ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
			defer cancel()
			v, e := startViewer(ctx, path, descriptor{SchemaVersion: 1, Profile: Profile, SessionID: "test"})
			if mode == "unknown" || mode == "duplicate" {
				if e == nil {
					v.Stop()
					t.Fatal("unsafe event accepted")
				}
				return
			}
			if e != nil {
				t.Fatal(e)
			}
			select {
			case <-v.FirstFrame():
			case <-ctx.Done():
				t.Fatal("missing frame")
			}
			e = v.Stop()
			if (e != nil) != (mode == "missing-stop") {
				t.Fatalf("stop outcome %v", e)
			}
		})
	}
}

func TestViewerRemovesInheritedRecordingAndDecodeOverrides(t *testing.T) {
	env := previewEnvironment([]string{"DISPLAY=:99", "XAUTHORITY=/tmp/test-auth", "PATH=/usr/bin", "USBRIDGE_FRAME_DUMP_DIR=/tmp/private", "USBRIDGE_FRAME_DUMP_EVERY_N=1", "USBRIDGE_PYROWAVE_DUMP=/tmp/frame.pgm", "USBRIDGE_SKIP_DECODE=1", "USBRIDGE_HWDEC=vaapi", "usbridge_log_dir=/tmp/logs"})
	if len(env) != 3 || env[0] != "DISPLAY=:99" || env[1] != "XAUTHORITY=/tmp/test-auth" || env[2] != "PATH=/usr/bin" {
		t.Fatalf("unsafe child environment: %v", env)
	}
}
