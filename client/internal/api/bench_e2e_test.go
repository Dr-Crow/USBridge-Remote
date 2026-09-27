package api

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// End-to-end check of the benchmark's monitor pin against a real agent on
// this machine, in the order runBenchmark makes the calls: pin a monitor,
// switch to each streamer, play the video, then release. Verifies each
// streamer's own config names the pinned monitor, Sunshine really captures
// it, the video's window is on it, and everything is put back afterwards.
//
// Opt-in (it restarts the host's streamers and plays video on its screens):
//
//	USBRIDGE_E2E_AGENT_STATE   agent state dir (%APPDATA%\usbridge-agent)
//	USBRIDGE_E2E_AGENT_EXEDIR  agent exe dir (holds rustshine\config)
//	USBRIDGE_E2E_MONITOR       monitor to pin, e.g. DISPLAY6
//	USBRIDGE_E2E_RUST_INDEX    rustshine monitor_index expected for it
//	USBRIDGE_E2E_SUN_ID        Sunshine output_name (device_id) expected
//	USBRIDGE_E2E_SUN_GPU       adapter Sunshine must report capturing on
func TestBenchMonitorPinEndToEnd(t *testing.T) {
	state := os.Getenv("USBRIDGE_E2E_AGENT_STATE")
	if state == "" {
		t.Skip("set USBRIDGE_E2E_AGENT_STATE and friends to run against the real agent")
	}
	exeDir := os.Getenv("USBRIDGE_E2E_AGENT_EXEDIR")
	target := `\\.\` + os.Getenv("USBRIDGE_E2E_MONITOR")
	rustConf := filepath.Join(exeDir, "rustshine", "config", "sunshine.conf")
	sunConf := filepath.Join(state, "sunshine", "sunshine.conf")
	sunLog := filepath.Join(state, "sunshine", "sunshine.log")

	cfg, err := os.ReadFile(filepath.Join(state, "config.yaml"))
	if err != nil {
		t.Fatal(err)
	}
	key := confValue(string(cfg), "master_key", ":")
	key = strings.Trim(key, `'"`)
	c := NewUSBClientWithScheme("http", "127.0.0.1", 8080, 30, nil)
	c.SetAPISecretV2([]byte(key))

	st, err := c.BenchStatus()
	if err != nil {
		t.Fatalf("status: %v", err)
	}
	t.Logf("active %s, monitors %+v", st.ActiveBackend, st.Monitors)
	found := false
	for _, m := range st.Monitors {
		found = found || m.ID == target
	}
	if !found {
		t.Fatalf("agent doesn't list monitor %s", target)
	}
	original := st.ActiveBackend
	origRust, origSun := readConf(t, rustConf, "monitor_index"), readConf(t, sunConf, "output_name")
	t.Logf("before: rustshine monitor_index=%q, sunshine output_name=%q", origRust, origSun)

	if err := c.BenchSetMonitor(target); err != nil {
		t.Fatalf("pin: %v", err)
	}
	released := false
	defer func() {
		if !released {
			_ = c.BenchSetMonitor("")
			_, _ = c.BenchSetBackend(original)
		}
	}()

	for _, kind := range []string{"rustshine", "sunshine"} {
		if _, err := c.BenchSetBackend(kind); err != nil {
			t.Fatalf("switch to %s: %v", kind, err)
		}
		switch kind {
		case "rustshine":
			if got := readConf(t, rustConf, "monitor_index"); got != os.Getenv("USBRIDGE_E2E_RUST_INDEX") {
				t.Errorf("rustshine monitor_index %q, want %q", got, os.Getenv("USBRIDGE_E2E_RUST_INDEX"))
			}
		case "sunshine":
			if got := readConf(t, sunConf, "output_name"); got != os.Getenv("USBRIDGE_E2E_SUN_ID") {
				t.Errorf("sunshine output_name %q, want %q", got, os.Getenv("USBRIDGE_E2E_SUN_ID"))
			}
			if gpu := lastCaptureAdapter(t, sunLog); gpu != os.Getenv("USBRIDGE_E2E_SUN_GPU") {
				t.Errorf("Sunshine captures on %q, want %q", gpu, os.Getenv("USBRIDGE_E2E_SUN_GPU"))
			} else {
				t.Logf("Sunshine captures on %s", gpu)
			}
		}
		v, err := c.BenchVideoStart()
		if err != nil {
			t.Fatalf("%s video: %v", kind, err)
		}
		t.Logf("%s: video %s requested on %s, window on %s", kind, v.Content, v.RequestedMonitor, v.Monitor)
		if v.RequestedMonitor != target || v.Monitor != target {
			t.Errorf("%s: video requested on %q, window on %q, want both %s", kind, v.RequestedMonitor, v.Monitor, target)
		}
		if err := c.BenchVideoStop(); err != nil {
			t.Fatalf("stop video: %v", err)
		}
	}

	if err := c.BenchSetMonitor(""); err != nil {
		t.Fatalf("release: %v", err)
	}
	if _, err := c.BenchSetBackend(original); err != nil {
		t.Fatalf("restore %s: %v", original, err)
	}
	released = true
	if got := readConf(t, rustConf, "monitor_index"); got != origRust {
		t.Errorf("rustshine monitor_index %q after release, want %q", got, origRust)
	}
	if got := readConf(t, sunConf, "output_name"); got != origSun {
		t.Errorf("sunshine output_name %q after release, want %q", got, origSun)
	}
	if st, err := c.BenchStatus(); err != nil || st.Monitor != "" || st.ActiveBackend != original {
		t.Errorf("after release: status %+v, err %v", st, err)
	}
}

func confValue(text, key, sep string) string {
	for _, line := range strings.Split(text, "\n") {
		k, v, ok := strings.Cut(line, sep)
		if ok && strings.TrimSpace(k) == key {
			return strings.TrimSpace(v)
		}
	}
	return ""
}

func readConf(t *testing.T, path, key string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil && !os.IsNotExist(err) {
		t.Fatal(err)
	}
	return confValue(string(data), key, "=")
}

var deviceDescRe = regexp.MustCompile(`Device Description\s*:\s*(.+)`)

// lastCaptureAdapter is the adapter of the last capture Sunshine logged.
func lastCaptureAdapter(t *testing.T, path string) string {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	m := deviceDescRe.FindAllStringSubmatch(string(data), -1)
	if len(m) == 0 {
		return ""
	}
	return strings.TrimSpace(m[len(m)-1][1])
}
