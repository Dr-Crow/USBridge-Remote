//go:build windows && graphicsprobe

// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

const graphicsProbeRole = "owned-wgl-probe-only"

type graphicsProbeResult struct {
	Schema   int    `json:"schema_version"`
	Role     string `json:"role"`
	Vendor   string `json:"gl_vendor"`
	Renderer string `json:"gl_renderer"`
	Version  string `json:"gl_version"`
}

type graphicsProbeReceipt struct {
	Schema            int                      `json:"schema_version"`
	Role              string                   `json:"role"`
	Passed            bool                     `json:"passed"`
	Failure           string                   `json:"failure_code,omitempty"`
	GraphicsRejection *graphicsModuleRejection `json:"graphics_module_rejection,omitempty"`
	ProbeSHA          string                   `json:"probe_sha256"`
	StagingSHA        string                   `json:"staging_sha256"`
	Vendor            string                   `json:"gl_vendor,omitempty"`
	Renderer          string                   `json:"gl_renderer,omitempty"`
	Version           string                   `json:"gl_version,omitempty"`
	Modules           map[string]string        `json:"loaded_runtime_dlls_sha256"`
	FixedDriver       string                   `json:"fixed_driver"`
	FixedSoftware     bool                     `json:"fixed_software"`
	Natural           bool                     `json:"natural_cleanup"`
	JobEmpty          bool                     `json:"job_empty_before_safety_close"`
	SafetyKill        bool                     `json:"safety_job_kill_used"`
	ActualViewer      bool                     `json:"actual_viewer_tested"`
	ActualPixels      bool                     `json:"actual_window_pixels_tested"`
	Capture           bool                     `json:"desktop_capture_tested"`
	Input             bool                     `json:"input_injection_tested"`
}

func graphicsASCII(value string) bool {
	if len(value) == 0 || len(value) > 255 {
		return false
	}
	for _, b := range []byte(value) {
		if b < 32 || b > 126 {
			return false
		}
	}
	return true
}

func parseGraphicsProbe(raw []byte) (graphicsProbeResult, error) {
	var r graphicsProbeResult
	if len(raw) > 2048 || exactJSON(raw, &r, "schema_version", "role", "gl_vendor", "gl_renderer", "gl_version") != nil ||
		r.Schema != 1 || r.Role != graphicsProbeRole || !graphicsASCII(r.Vendor) ||
		!graphicsASCII(r.Renderer) || !graphicsASCII(r.Version) ||
		!strings.HasPrefix(r.Renderer, "llvmpipe (") || !strings.HasSuffix(r.Renderer, ")") ||
		!strings.Contains(r.Version, "Mesa ") {
		return graphicsProbeResult{}, failure("invalid_software_graphics_result")
	}
	return r, nil
}

func executeGraphicsProbe(probe, probeSHA, staging, stagingSHA, work string, r *graphicsProbeReceipt) (result error) {
	pins := new(pins)
	defer pins.close()
	if _, err := pins.check(probe, probeSHA, 32<<20); err != nil {
		return err
	}
	expected, err := pinGraphicsStaging(pins, probe, staging, stagingSHA)
	if err != nil {
		return err
	}
	root := os.Getenv("SystemRoot")
	if !drivePath.MatchString(root) || strings.ContainsAny(root, ";\x00\r\n") {
		return failure("invalid_system_root")
	}
	j, err := newJob()
	if err != nil {
		return err
	}
	defer j.close()
	watchdog := time.AfterFunc(30*time.Second, j.close)
	defer watchdog.Stop()
	p, err := j.start(probe, nil, childEnvironment(root, work), work)
	if err != nil {
		return err
	}
	defer func() {
		ids, err := j.pids()
		if j.closed.Load() || err != nil || len(ids) != 0 || p.alive() {
			r.SafetyKill, r.Natural, r.JobEmpty = true, false, false
			j.close()
			_ = p.wait(3 * time.Second)
			if result == nil {
				result = failure("graphics_safety_cleanup_required")
			}
		}
		p.close()
	}()
	raw, err := p.next(12 * time.Second)
	if err != nil {
		return err
	}
	observed, err := parseGraphicsProbe(raw)
	clear(raw)
	if err != nil {
		return err
	}
	ids, err := j.pids()
	if err != nil || len(ids) != 1 || ids[0] != p.pid || !p.alive() || !j.contains(p.handle) {
		return failure("graphics_owned_process_unverified")
	}
	modules, err := graphicsModules(p.pid, probe, root, expected, nil, nil)
	if err != nil {
		return err
	}
	r.Vendor, r.Renderer, r.Version = observed.Vendor, observed.Renderer, observed.Version
	r.Modules = modules
	if err := p.stdin.Close(); err != nil {
		return failure("graphics_private_eof_failed")
	}
	if err := p.wait(4 * time.Second); err != nil {
		return err
	}
	if err := p.finishProtocol(); err != nil {
		return err
	}
	if err := waitRetiredInventory(j.pids, map[uint32]bool{p.pid: true}, 3*time.Second); err != nil {
		return err
	}
	r.Natural, r.JobEmpty = true, true
	return nil
}

func TestWindowsOwnedSoftwareGraphicsProbe(t *testing.T) {
	keys := []string{"WINDOWS_GRAPHICS_PROBE", "WINDOWS_GRAPHICS_PROBE_SHA256", "WINDOWS_GRAPHICS_PROBE_STAGING", "WINDOWS_GRAPHICS_PROBE_STAGING_SHA256", "WINDOWS_GRAPHICS_PROBE_RECEIPT"}
	values := make([]string, len(keys))
	for i, key := range keys {
		values[i] = os.Getenv(key)
		if values[i] == "" {
			t.Fatal("graphics_probe_configuration_missing")
		}
	}
	probe, probeSHA, staging, stagingSHA, output := values[0], values[1], values[2], values[3], values[4]
	for _, path := range []string{probe, staging, output} {
		if !drivePath.MatchString(path) || strings.ContainsAny(path, "\x00\r\n") || strings.Contains(path[2:], ":") {
			t.Fatal("invalid_graphics_probe_path")
		}
	}
	if !hashPattern.MatchString(probeSHA) || !hashPattern.MatchString(stagingSHA) || filepath.Base(probe) != "windows-graphics-probe.exe" {
		t.Fatal("invalid_graphics_probe_identity")
	}
	if _, err := os.Lstat(output); !os.IsNotExist(err) {
		t.Fatal("graphics_probe_receipt_must_be_new")
	}
	r := graphicsProbeReceipt{Schema: 1, Role: graphicsProbeRole, ProbeSHA: probeSHA, StagingSHA: stagingSHA,
		FixedDriver: "llvmpipe", FixedSoftware: true, Modules: map[string]string{}}
	if err := executeGraphicsProbe(probe, probeSHA, staging, stagingSHA, t.TempDir(), &r); err != nil {
		r.Failure = err.Error()
		r.GraphicsRejection = graphicsRejection(err)
	} else {
		r.Passed = true
	}
	raw, err := json.MarshalIndent(r, "", "  ")
	if err != nil || writeReceipt(output, append(raw, '\n')) != nil {
		t.Fatal("graphics_probe_receipt_write_failed")
	}
	if !r.Passed {
		t.Fatal(r.Failure)
	}
}

func TestGraphicsProbeStrictParsers(t *testing.T) {
	valid := `{"schema_version":1,"role":"owned-wgl-probe-only","gl_vendor":"Mesa","gl_renderer":"llvmpipe (LLVM 20.1.8, 256 bits)","gl_version":"4.5 Mesa 26.2.4"}`
	if _, err := parseGraphicsProbe([]byte(valid)); err != nil {
		t.Fatal("valid graphics result rejected")
	}
	for _, wrong := range []string{
		valid + `{}`, strings.Replace(valid, `"schema_version":1`, `"schema_version":1,"schema_version":1`, 1),
		strings.Replace(valid, `"gl_vendor":"Mesa"`, `"GL_VENDOR":"Mesa"`, 1),
		strings.Replace(valid, `"gl_vendor":"Mesa"`, `"gl_vendor":null`, 1),
		strings.Replace(valid, `llvmpipe (LLVM 20.1.8, 256 bits)`, `D3D12 (GPU)`, 1),
		strings.Replace(valid, `Mesa`, `Mesa\n`, 1), strings.Replace(valid, `Mesa`, strings.Repeat("a", 256), 1),
	} {
		if _, err := parseGraphicsProbe([]byte(wrong)); err == nil {
			t.Fatal("invalid graphics result accepted")
		}
	}
	digest := strings.Repeat("a", 64)
	staging := `{"schema_version":1,"runtime_dlls_sha256":{"opengl32.dll":"` + digest + `","libgallium_wgl.dll":"` + digest + `"}}`
	if _, err := parseGraphicsStaging([]byte(staging)); err != nil {
		t.Fatal("valid staging rejected")
	}
	for _, wrong := range []string{
		staging + `{}`, strings.Replace(staging, `"opengl32.dll":`, `"../opengl32.dll":`, 1),
		strings.Replace(staging, `"opengl32.dll":`, `"opengl32.dll":"`+digest+`","OPENGL32.dll":`, 1),
		strings.Replace(staging, `"libgallium_wgl.dll"`, `"other.dll"`, 1),
		strings.Replace(staging, digest, strings.Repeat("g", 64), 1),
		strings.Replace(staging, `"schema_version":1`, `"schema_version":1,"extra":false`, 1),
	} {
		if _, err := parseGraphicsStaging([]byte(wrong)); err == nil {
			t.Fatal("invalid staging accepted")
		}
	}
}
