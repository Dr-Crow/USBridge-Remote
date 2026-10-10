//go:build windows

// SPDX-License-Identifier: GPL-3.0-only
package main

import (
	"encoding/json"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"time"
)

type pins struct {
	files       []*os.File
	sourceEntry string
}

func (p *pins) close() {
	for _, f := range p.files {
		_ = f.Close()
	}
}
func (p *pins) check(path, pin string, limit int64) (*os.File, error) {
	if !hashPattern.MatchString(pin) {
		return nil, failure("invalid_component_pin")
	}
	f, e := lockFile(path)
	if e != nil {
		return nil, e
	}
	p.files = append(p.files, f)
	got, e := fileSHA(f, limit)
	if e != nil || got != pin {
		return nil, failure("component_hash_mismatch")
	}
	return f, nil
}
func readPinned(f *os.File, limit int64) ([]byte, error) {
	if _, e := f.Seek(0, io.SeekStart); e != nil {
		return nil, failure("manifest_read_failed")
	}
	b, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(b)) > limit {
		return nil, failure("manifest_read_failed")
	}
	return b, nil
}
func safeRelative(p string) bool {
	return p != "" && !strings.ContainsAny(p, "\\:%") && filepath.IsLocal(filepath.FromSlash(p)) && filepath.ToSlash(filepath.Clean(filepath.FromSlash(p))) == p
}
func preflight(c config) (*pins, error) {
	p := new(pins)
	ok := false
	defer func() {
		if !ok {
			p.close()
		}
	}()
	for _, v := range []struct{ path, pin string }{{c.Agent, c.AgentSHA}, {c.Viewer, c.ViewerSHA}, {c.Fixture, c.FixtureSHA}} {
		if _, e := p.check(v.path, v.pin, 512<<20); e != nil {
			return nil, e
		}
	}
	f, e := p.check(filepath.Join(c.Components, "manifest.json"), c.ManifestSHA, 1<<20)
	if e != nil {
		return nil, e
	}
	raw, e := readPinned(f, 1<<20)
	if e != nil {
		return nil, e
	}
	var manifest struct {
		Schema     int `json:"schema"`
		Components []struct {
			Name, Platform, Version, Profile, Entry string
			Files                                   []struct {
				Path, SHA256 string
				Size         int64
				Executable   bool
			}
		}
	}
	if json.Unmarshal(raw, &manifest) != nil || manifest.Schema != 1 {
		return nil, failure("invalid_component_manifest")
	}
	found := false
	for _, comp := range manifest.Components {
		if comp.Name != "source-streamer" || comp.Platform != "windows/amd64" {
			continue
		}
		if found || comp.Version != sourceCommit || comp.Profile != "source-streamer-v1" || !safeRelative(comp.Entry) {
			return nil, failure("source_provenance_mismatch")
		}
		found = true
		p.sourceEntry = comp.Entry
		entryFound := false
		for _, file := range comp.Files {
			if !safeRelative(file.Path) || file.Size < 1 {
				return nil, failure("invalid_component_file")
			}
			f, e = p.check(filepath.Join(c.Components, filepath.FromSlash(file.Path)), file.SHA256, 512<<20)
			if e != nil {
				return nil, e
			}
			s, e := f.Stat()
			if e != nil || s.Size() != file.Size {
				return nil, failure("component_size_mismatch")
			}
			if file.Path == comp.Entry {
				if entryFound || file.SHA256 != c.SourceSHA || !file.Executable {
					return nil, failure("source_entry_mismatch")
				}
				entryFound = true
			}
		}
		if !entryFound {
			return nil, failure("source_entry_missing")
		}
	}
	if !found {
		return nil, failure("source_component_missing")
	}
	f, e = p.check(filepath.Join(filepath.Dir(c.Fixture), "fixture-assets.json"), c.FixtureManifestSHA, 65536)
	if e != nil {
		return nil, e
	}
	raw, e = readPinned(f, 65536)
	if e != nil {
		return nil, e
	}
	var assets struct {
		Schema         int    `json:"schema_version"`
		Role           string `json:"role"`
		DesktopCapture *bool  `json:"desktop_capture_tested"`
		SourceCommit   string `json:"source_commit"`
		Assets         map[string]struct {
			SHA256 string `json:"sha256"`
			Bytes  int64  `json:"bytes"`
		}
	}
	if json.Unmarshal(raw, &assets) != nil || assets.Schema != 1 || assets.Role != "synthetic-substitution-only" || assets.DesktopCapture == nil || *assets.DesktopCapture || assets.SourceCommit != sourceCommit || len(assets.Assets) != 3 {
		return nil, failure("invalid_fixture_manifest")
	}
	for _, name := range []string{"fixture-blue.h264", "fixture-orange.h264", "fixture-silence.ogg"} {
		a, present := assets.Assets[name]
		limit := int64(64 << 10)
		if name == "fixture-silence.ogg" {
			limit = 1 << 20
		}
		if !present || a.Bytes <= 0 || a.Bytes > limit {
			return nil, failure("invalid_fixture_asset")
		}
		f, e = p.check(filepath.Join(filepath.Dir(c.Fixture), name), a.SHA256, limit)
		if e != nil {
			return nil, e
		}
		s, e := f.Stat()
		if e != nil || s.Size() != a.Bytes {
			return nil, failure("fixture_asset_size_mismatch")
		}
	}
	ok = true
	return p, nil
}
func execute(c config, r *receipt) error {
	if runtime.GOARCH != "amd64" {
		return failure("native_windows_amd64_required")
	}
	// Refuse to reuse a previous case's state, Fyne preferences or media files.
	if _, e := os.Lstat(c.Work); !os.IsNotExist(e) {
		return failure("work_directory_must_be_new")
	}
	if e := os.Mkdir(c.Work, 0700); e != nil {
		return failure("work_directory_failed")
	}
	p, e := preflight(c)
	if e != nil {
		return e
	}
	defer p.close()
	root := os.Getenv("SystemRoot")
	if !drivePath.MatchString(root) || strings.ContainsAny(root, ";\x00\r\n") {
		return failure("invalid_system_root")
	}
	for _, mode := range []string{"window_close", "stdin_eof", "stdin_extra"} {
		cr := caseReceipt{Name: mode}
		e = runCase(c, root, p.sourceEntry, &cr)
		r.Cases = append(r.Cases, cr)
		if e != nil {
			return e
		}
	}
	r.ActualAgentCLI = true
	r.ActualMedia = true
	r.ActualPixels = true
	r.SystemOnlyPath = true
	r.JobContainment = true
	r.FreshRestart = true
	return nil
}

type ownedProcess struct {
	handle syscall.Handle
	role   string
}
type inventory struct {
	known                   map[uint32]ownedProcess
	hashes                  map[string]string
	sourceSeen, fixtureSeen bool
}

func (i *inventory) close() {
	for _, p := range i.known {
		_ = syscall.CloseHandle(p.handle)
	}
}
func (i *inventory) sample(j *job) (map[string]int, error) {
	pids, e := j.pids()
	if e != nil {
		return nil, e
	}
	counts := map[string]int{}
	for _, pid := range pids {
		p, known := i.known[pid]
		if !known {
			path, h, e := processPath(pid)
			if e != nil {
				return nil, e
			}
			if !j.contains(h) {
				syscall.CloseHandle(h)
				return nil, failure("process_outside_owned_job")
			}
			f, e := lockFile(path)
			if e != nil {
				syscall.CloseHandle(h)
				return nil, e
			}
			hash, e := fileSHA(f, 512<<20)
			f.Close()
			role, allowed := i.hashes[hash]
			if e != nil || !allowed {
				syscall.CloseHandle(h)
				return nil, failure("unexpected_owned_executable")
			}
			p = ownedProcess{h, role}
			i.known[pid] = p
		}
		counts[p.role]++
	}
	if counts["agent"] > 1 || counts["source"] > 1 || counts["viewer"] > 1 || counts["fixture"] > 2 {
		return nil, failure("unexpected_owned_process_count")
	}
	if counts["source"] == 1 {
		i.sourceSeen = true
	}
	if counts["fixture"] == 2 {
		i.fixtureSeen = true
	}
	return counts, nil
}
func (i *inventory) allExited(timeout time.Duration) error {
	end := time.Now().Add(timeout)
	for _, p := range i.known {
		left := time.Until(end)
		if left <= 0 {
			return failure("owned_process_exit_timeout")
		}
		result, e := syscall.WaitForSingleObject(p.handle, uint32(left/time.Millisecond))
		if e != nil || result != syscall.WAIT_OBJECT_0 {
			return failure("owned_process_exit_timeout")
		}
	}
	return nil
}
func runCase(c config, root, sourceEntry string, cr *caseReceipt) (result error) {
	caseStarted := time.Now()
	work := filepath.Join(c.Work, cr.Name)
	for _, dir := range []string{work, filepath.Join(work, "roaming"), filepath.Join(work, "local")} {
		if os.Mkdir(dir, 0700) != nil {
			return failure("case_directory_failed")
		}
	}
	j, e := newJob()
	if e != nil {
		return e
	}
	children := []*child{}
	inv := inventory{known: map[uint32]ownedProcess{}, hashes: map[string]string{c.AgentSHA: "agent", c.ViewerSHA: "viewer", c.SourceSHA: "source", c.FixtureSHA: "fixture"}}
	// An independent hard lease remains even if a Win32 call fails to return.
	watchdog := time.AfterFunc(40*time.Second, j.close)
	defer func() {
		watchdog.Stop()
		if pids, e := j.pids(); e != nil || len(pids) != 0 {
			cr.SafetyKillUsed = true
			if result == nil {
				result = failure("safety_cleanup_required")
				cr.Passed = false
				cr.NaturalCleanup = false
			}
		}
		j.close()
		for _, p := range children {
			_ = p.wait(5 * time.Second)
			p.close()
		}
		if e := inv.allExited(5 * time.Second); result == nil && e != nil {
			result = e
			cr.Passed = false
		}
		inv.close()
	}()
	env := childEnvironment(root, work)
	agent, e := j.start(c.Agent, []string{"--source-streamer-mode", "--source-component-directory", c.Components, "--source-manifest-sha256", c.ManifestSHA, "--source-state-dir", filepath.Join(work, "state")}, env, work)
	if e != nil {
		return e
	}
	children = append(children, agent)
	id, launch, descriptor, e := privatePayloads(c.Fixture)
	if e != nil {
		return e
	}
	defer delete(launch, "key_b64")
	defer delete(descriptor, "key_b64")
	if e = writePrivate(agent.stdin, launch); e != nil {
		return e
	}
	delete(launch, "key_b64")
	raw, e := agent.next(10 * time.Second)
	if e != nil {
		return e
	}
	ready, e := parseReady(raw, id)
	clear(raw)
	if e != nil {
		return e
	}
	// Recheck and hold the agent-staged source executable as well as its original.
	staged, e := lockFile(filepath.Join(work, "state", "local-components", "source-streamer", filepath.FromSlash(sourceEntry)))
	if e != nil {
		return e
	}
	defer staged.Close()
	hash, e := fileSHA(staged, 512<<20)
	if e != nil || hash != c.SourceSHA {
		return failure("staged_source_hash_mismatch")
	}
	viewer, e := j.start(c.Viewer, []string{"--source-preview-stdin"}, env, work)
	if e != nil {
		return e
	}
	children = append(children, viewer)
	descriptor["rtsp_url"] = "rtspenc://" + ready.RTSP
	expires := time.Now().UTC().Add(28 * time.Second)
	descriptor["expires_at"] = expires
	if e = writePrivate(viewer.stdin, descriptor); e != nil {
		return e
	}
	delete(descriptor, "key_b64")
	for _, kind := range []string{"ready", "first_frame"} {
		raw, e = viewer.next(12 * time.Second)
		if e != nil {
			return e
		}
		e = parseViewer(raw, id, kind, "")
		clear(raw)
		if e != nil {
			return e
		}
	}
	cr.FirstFrame = true
	var hwnd uintptr
	lastColor := 0
	deadline := time.Now().Add(7 * time.Second)
	for time.Now().Before(deadline) {
		if !agent.alive() || !viewer.alive() {
			return failure("child_exited_before_pixel_gate")
		}
		counts, e := inv.sample(j)
		if e != nil {
			return e
		}
		if counts["fixture"] > cr.FixtureProcesses {
			cr.FixtureProcesses = counts["fixture"]
		}
		current, snapshot, e := ownedWindowSnapshot(viewer.pid)
		if e != nil {
			return e
		}
		cr.OwnedWindows = max(cr.OwnedWindows, snapshot.Owned)
		cr.VisibleWindows = max(cr.VisibleWindows, snapshot.Visible)
		cr.TitleMatches = max(cr.TitleMatches, snapshot.TitleMatches)
		if current != 0 {
			if hwnd != 0 && hwnd != current {
				return failure("owned_window_identity_changed")
			}
			hwnd = current
			color, e := boundedPixels(hwnd, viewer.pid)
			if e != nil {
				return e
			}
			cr.PixelSamples++
			if color > 0 {
				if lastColor != 0 && lastColor != color {
					cr.ColorTransitions++
				}
				lastColor = color
				if color == 1 {
					cr.Blue = true
				} else {
					cr.Orange = true
				}
			}
		}
		if cr.Blue && cr.Orange && cr.ColorTransitions >= 2 && inv.sourceSeen && inv.fixtureSeen {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if hwnd == 0 || !cr.Blue || !cr.Orange || cr.ColorTransitions < 2 {
		return failure("owned_window_changing_pixels_unverified")
	}
	if !inv.sourceSeen || !inv.fixtureSeen {
		return failure("generated_media_processes_unverified")
	}
	// Establish a generous margin from both independent leases so a generic
	// stopped.completed cannot be expiry masquerading as our close/EOF action.
	if time.Until(expires) < 10*time.Second || time.Since(caseStarted) > 18*time.Second || !agent.alive() || !viewer.alive() {
		return failure("stop_action_deadline_margin")
	}
	select {
	case packet, ok := <-viewer.packets:
		if ok {
			clear(packet.line)
		}
		return failure("viewer_stopped_before_action")
	default:
	}
	// Each successful case naturally tears down before a new key/session starts.
	expected := "completed"
	switch cr.Name {
	case "window_close":
		e = closeWindow(hwnd, viewer.pid)
	case "stdin_eof":
		e = viewer.stdin.Close()
	case "stdin_extra":
		expected = "failed"
		_, e = viewer.stdin.Write([]byte{0x78})
	default:
		return failure("unknown_case")
	}
	if e != nil {
		return failure("stop_request_failed")
	}
	raw, e = viewer.next(6 * time.Second)
	if e != nil {
		return e
	}
	e = parseViewer(raw, id, "stopped", expected)
	clear(raw)
	if e != nil {
		return e
	}
	if e = viewer.wait(4 * time.Second); e != nil {
		return e
	}
	if e = viewer.finishProtocol(); e != nil {
		return e
	}
	if e = agent.stdin.Close(); e != nil {
		return failure("source_lease_close_failed")
	}
	raw, e = agent.next(5 * time.Second)
	if e != nil {
		return e
	}
	s, e := parseStopped(raw, id)
	clear(raw)
	if e != nil {
		return e
	}
	cr.VideoFrames = s.VideoFrames
	cr.AudioPackets = s.AudioPackets
	if e = agent.wait(4 * time.Second); e != nil {
		return e
	}
	if e = agent.finishProtocol(); e != nil {
		return e
	}
	if e = inv.allExited(3 * time.Second); e != nil {
		return e
	}
	retired := make(map[uint32]bool, len(inv.known))
	for pid := range inv.known {
		retired[pid] = true
	}
	if e = waitRetiredInventory(j.pids, retired, 3*time.Second); e != nil {
		return e
	}
	cr.JobEmpty = true
	// EnumWindows still binds exact PID and title. Never close any other HWND.
	current, e := ownedWindow(viewer.pid)
	if e != nil || current != 0 {
		return failure("owned_window_remained")
	}
	cr.WindowGone = true
	if e = listenersClosed(ready); e != nil {
		return e
	}
	cr.ListenersClosed = true
	cr.NaturalCleanup = true
	cr.Passed = true
	return nil
}
func listenersClosed(r readyEvent) error {
	conn, e := net.DialTimeout("tcp4", r.RTSP, 400*time.Millisecond)
	if e == nil {
		conn.Close()
		return failure("rtsp_listener_remained")
	}
	// Rebinding provides stronger evidence than a failed dial.
	tcp, e := net.Listen("tcp4", r.RTSP)
	if e != nil {
		return failure("rtsp_port_not_released")
	}
	tcp.Close()
	udp, e := net.ListenPacket("udp4", r.Control)
	if e != nil {
		return failure("control_port_not_released")
	}
	udp.Close()
	return nil
}
