//go:build windows && source_preview_manager_acceptance

// SPDX-License-Identifier: GPL-3.0-only
package sourcepreview

// This opt-in gate calls the production Manager and production launchers. The
// only encoder it permits is the independently built, hash-pinned generated
// fixture. It never constructs App, captures a desktop, injects input, replaces
// a permission predicate, changes identity, or writes observed pixels.
import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"usbridge_agent/internal/componentjson"
	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/previeweligibility"
	"usbridge_agent/internal/sourcestreamer"
)

const managerFixtureSource = "2e07af3484369bc68bc8091d5a04969867eff0f9"

func managerFailure(code string) error { return errors.New(code) }

// Cleanup observations never replace the primary fixed failure category or
// expose a native error. Uncertainty stays sticky even if a later Stop returns.
type managerCleanupError struct {
	primary   error
	codes     []string
	uncertain bool
}

func (e *managerCleanupError) Error() string {
	if e.primary != nil {
		return e.primary.Error()
	}
	return "manager_cleanup_failed"
}
func (e *managerCleanupError) Unwrap() error { return e.primary }

func managerCleanupCode(codes []string, code string) []string {
	for _, existing := range codes {
		if existing == code {
			return codes
		}
	}
	return append(codes, code)
}

func managerWithCleanup(primary error, code string, uncertain bool) error {
	next := &managerCleanupError{primary: primary, uncertain: uncertain}
	if previous, ok := primary.(*managerCleanupError); ok {
		next.primary = previous.primary
		next.codes = append([]string(nil), previous.codes...)
		next.uncertain = next.uncertain || previous.uncertain
	}
	next.codes = managerCleanupCode(next.codes, code)
	return next
}

func managerMergeCleanup(primary, cleanup error) error {
	if cleanup == nil {
		return primary
	}
	if primary == nil {
		if _, ok := cleanup.(*managerCleanupError); ok {
			return cleanup
		}
		return managerWithCleanup(cleanup, cleanup.Error(), false)
	}
	var detail *managerCleanupError
	uncertain := errors.As(cleanup, &detail) && detail.uncertain
	result := managerWithCleanup(primary, cleanup.Error(), uncertain)
	if detail != nil {
		for _, code := range detail.codes {
			result = managerWithCleanup(result, code, detail.uncertain)
		}
	}
	return result
}

func managerRecordFailure(r *managerNativeReceipt, failure error) {
	if failure == nil {
		return
	}
	r.Passed = false
	if r.Failure == "" {
		r.Failure = failure.Error()
	} else if r.Failure != failure.Error() {
		r.CleanupFailures = managerCleanupCode(r.CleanupFailures, failure.Error())
	}
	var detail *managerCleanupError
	if errors.As(failure, &detail) {
		r.CleanupUncertain = r.CleanupUncertain || detail.uncertain
		for _, code := range detail.codes {
			r.CleanupFailures = managerCleanupCode(r.CleanupFailures, code)
		}
	}
}

type managerNativeCase struct {
	Name             string   `json:"name"`
	Passed           bool     `json:"passed"`
	FirstFrame       bool     `json:"typed_first_frame"`
	Viewing          bool     `json:"manager_viewing"`
	Blue             bool     `json:"owned_window_blue"`
	Orange           bool     `json:"owned_window_orange"`
	Transitions      int      `json:"owned_window_color_transitions"`
	Samples          int      `json:"owned_window_samples"`
	OverlapRejected  bool     `json:"overlap_rejected_without_launch"`
	NaturalJoined    bool     `json:"natural_source_and_viewer_joined"`
	WindowGone       bool     `json:"owned_window_removed"`
	ListenersClosed  bool     `json:"advertised_ports_rebindable"`
	VideoFrames      uint64   `json:"source_video_frames"`
	AudioPackets     uint64   `json:"source_audio_packets"`
	StagedSourceSHA  string   `json:"staged_source_sha256,omitempty"`
	StagedViewerSHA  string   `json:"staged_viewer_sha256,omitempty"`
	StagedVerified   bool     `json:"staged_entry_hashes_verified"`
	CleanupFailures  []string `json:"cleanup_failure_codes,omitempty"`
	CleanupUncertain bool     `json:"cleanup_uncertain"`
}

type managerNativeReceipt struct {
	Schema               int                         `json:"schema_version"`
	Passed               bool                        `json:"passed"`
	Failure              string                      `json:"failure_code,omitempty"`
	CleanupFailures      []string                    `json:"cleanup_failure_codes,omitempty"`
	CleanupUncertain     bool                        `json:"cleanup_uncertain"`
	Platform             string                      `json:"platform"`
	Commit               string                      `json:"commit"`
	SourceCommit         string                      `json:"source_streamer_commit"`
	ManifestSHA          string                      `json:"manifest_sha256"`
	FixtureSHA           string                      `json:"fixture_sha256"`
	ManifestVerified     bool                        `json:"manifest_roles_and_hash_verified"`
	LoadedModulesProved  bool                        `json:"loaded_graphics_modules_independently_verified"`
	Eligibility          previeweligibility.Snapshot `json:"eligibility"`
	ActualManagerStarted bool                        `json:"actual_manager_started"`
	ActualAppNew         bool                        `json:"actual_app_new"`
	GeneratedCaptureOnly bool                        `json:"generated_capture_only"`
	FixturePinned        bool                        `json:"generated_fixture_pinned"`
	DesktopCapture       bool                        `json:"desktop_capture_tested"`
	InputInjection       bool                        `json:"input_injection_tested"`
	PixelsSaved          bool                        `json:"pixels_saved"`
	ArtifactsPublished   bool                        `json:"source_or_binary_artifacts_published"`
	ConsentRejected      bool                        `json:"missing_consent_rejected_without_launch"`
	FreshKeys            bool                        `json:"fresh_keys"`
	FreshSessions        bool                        `json:"fresh_session_ids"`
	FreshKeyIDs          bool                        `json:"fresh_key_ids"`
	SourceStarts         int                         `json:"source_starts"`
	ViewerStarts         int                         `json:"viewer_starts"`
	Cases                []managerNativeCase         `json:"cases"`
}

type managerNativeConfig struct {
	components, manifest, fixture, fixtureSHA, output, commit string
	sourceSHA, viewerSHA, sourceEntry, viewerEntry            string
}

func managerNativeConfigFromEnvironment() (managerNativeConfig, error) {
	c := managerNativeConfig{
		components: os.Getenv("SOURCE_PREVIEW_COMPONENTS"), manifest: os.Getenv("SOURCE_PREVIEW_MANIFEST_SHA256"),
		fixture: os.Getenv("SOURCE_PREVIEW_GENERATED_FIXTURE"), fixtureSHA: os.Getenv("SOURCE_PREVIEW_GENERATED_FIXTURE_SHA256"),
		output: os.Getenv("SOURCE_PREVIEW_MANAGER_OUTPUT"),
		commit: os.Getenv("CIRCLE_SHA1"),
	}
	if os.Getenv("SOURCE_PREVIEW_MANAGER_ACCEPTANCE") != "1" || runtime.GOOS != "windows" || runtime.GOARCH != "amd64" {
		return c, managerFailure("explicit_native_optin_required")
	}
	if !managerValidHash(c.manifest) || !managerValidHash(c.fixtureSHA) {
		return c, managerFailure("invalid_input_hash")
	}
	commit, commitErr := hex.DecodeString(c.commit)
	if commitErr != nil || len(commit) != 20 || c.commit != strings.ToLower(c.commit) {
		return c, managerFailure("exact_public_commit_required")
	}
	for _, p := range []*string{&c.components, &c.fixture, &c.output} {
		if !managerLocalPath(*p) {
			return c, managerFailure("absolute_local_input_required")
		}
		*p = filepath.Clean(filepath.FromSlash(*p))
		if err := managerPlainAncestry(*p); err != nil {
			return c, err
		}
	}
	if !strings.EqualFold(filepath.Base(c.fixture), "ffmpeg-fixture.exe") {
		return c, managerFailure("generated_fixture_name_required")
	}
	for _, dir := range []string{c.components, c.output} {
		info, err := os.Stat(dir)
		if err != nil || !info.IsDir() {
			return c, managerFailure("input_directory_required")
		}
	}
	entries, err := os.ReadDir(c.output)
	if err != nil || len(entries) != 0 {
		return c, managerFailure("fresh_output_directory_required")
	}
	return c, nil
}

func managerValidHash(s string) bool {
	b, err := hex.DecodeString(s)
	return err == nil && len(b) == sha256.Size && s == strings.ToLower(s)
}

func managerLocalPath(p string) bool {
	return len(p) > 3 && ((p[0] >= 'A' && p[0] <= 'Z') || (p[0] >= 'a' && p[0] <= 'z')) &&
		p[1] == ':' && (p[2] == '\\' || p[2] == '/') && !strings.ContainsAny(p[3:], ":\x00\r\n") && filepath.IsAbs(p)
}

// Refuse reparse points in every existing ancestor as well as the final file.
func managerPlainAncestry(p string) error {
	for {
		name, err := syscall.UTF16PtrFromString(p)
		if err != nil {
			return managerFailure("input_path_invalid")
		}
		attributes, err := syscall.GetFileAttributes(name)
		if err != nil || attributes&syscall.FILE_ATTRIBUTE_REPARSE_POINT != 0 {
			return managerFailure("input_reparse_or_missing")
		}
		parent := filepath.Dir(p)
		if parent == p {
			return nil
		}
		p = parent
	}
}

// FILE_SHARE_READ denies replacement, writes and deletion while execution and
// cleanup run. Use the held file for both size and digest observations.
func managerHoldFile(path, want string, limit int64) (file *os.File, result error) {
	if err := managerPlainAncestry(path); err != nil {
		return nil, err
	}
	before, err := os.Lstat(path)
	if err != nil || !before.Mode().IsRegular() || before.Size() <= 0 || before.Size() > limit {
		return nil, managerFailure("input_file_invalid")
	}
	p, err := syscall.UTF16PtrFromString(path)
	if err != nil {
		return nil, managerFailure("input_path_invalid")
	}
	handle, err := syscall.CreateFile(p, syscall.GENERIC_READ, syscall.FILE_SHARE_READ, nil, syscall.OPEN_EXISTING, syscall.FILE_ATTRIBUTE_NORMAL, 0)
	if err != nil {
		return nil, managerFailure("input_lock_failed")
	}
	f := os.NewFile(uintptr(handle), "manager-verified-input")
	ok := false
	defer func() {
		if !ok {
			if f.Close() != nil {
				result = managerWithCleanup(result, "input_handle_close_failed", true)
			}
		}
	}()
	after, err := f.Stat()
	if err != nil || !os.SameFile(before, after) || after.Size() != before.Size() {
		return nil, managerFailure("input_identity_changed")
	}
	if want != "" {
		h := sha256.New()
		n, err := io.Copy(h, io.LimitReader(f, limit+1))
		if err != nil || n != after.Size() || !managerValidHash(want) || hex.EncodeToString(h.Sum(nil)) != want {
			return nil, managerFailure("input_hash_mismatch")
		}
	}
	if _, err := f.Seek(0, io.SeekStart); err != nil {
		return nil, managerFailure("input_seek_failed")
	}
	ok = true
	return f, nil
}

func managerHoldFixture(c managerNativeConfig) (files []*os.File, result error) {
	var held []*os.File
	ok := false
	defer func() {
		if !ok {
			for _, f := range held {
				if f.Close() != nil {
					result = managerWithCleanup(result, "fixture_handle_close_failed", true)
				}
			}
		}
	}()
	fixture, err := managerHoldFile(c.fixture, c.fixtureSHA, 64<<20)
	if err != nil {
		return nil, err
	}
	held = append(held, fixture)
	manifest, err := managerHoldFile(filepath.Join(filepath.Dir(c.fixture), "fixture-assets.json"), "", 65536)
	if err != nil {
		return nil, err
	}
	held = append(held, manifest)
	raw, err := io.ReadAll(io.LimitReader(manifest, 65537))
	var assets struct {
		Schema         int    `json:"schema_version"`
		Role           string `json:"role"`
		DesktopCapture *bool  `json:"desktop_capture_tested"`
		SourceCommit   string `json:"source_commit"`
		Assets         map[string]struct {
			SHA256 string `json:"sha256"`
			Bytes  int64  `json:"bytes"`
		} `json:"assets"`
	}
	if err != nil || len(raw) > 65536 || json.Unmarshal(raw, &assets) != nil || assets.Schema != 1 ||
		assets.Role != "synthetic-substitution-only" || assets.DesktopCapture == nil || *assets.DesktopCapture ||
		assets.SourceCommit != managerFixtureSource || len(assets.Assets) != 3 {
		return nil, managerFailure("generated_fixture_manifest_invalid")
	}
	for _, name := range []string{"fixture-blue.h264", "fixture-orange.h264", "fixture-silence.ogg"} {
		a, present := assets.Assets[name]
		limit := int64(64 << 10)
		if name == "fixture-silence.ogg" {
			limit = 1 << 20
		}
		if !present || a.Bytes <= 0 || a.Bytes > limit {
			return nil, managerFailure("generated_asset_invalid")
		}
		f, err := managerHoldFile(filepath.Join(filepath.Dir(c.fixture), name), a.SHA256, limit)
		if err != nil {
			return nil, err
		}
		held = append(held, f)
		info, err := f.Stat()
		if err != nil || info.Size() != a.Bytes {
			return nil, managerFailure("generated_asset_size_mismatch")
		}
	}
	ok = true
	return held, nil
}

// Bind both component roles to this public build and the frozen public source.
// The supplied manifest digest is the trust input; filenames/version claims
// alone never authorize an executable. Resolve still independently verifies
// every listed file before it stages and launches each production child.
func managerHoldManifest(c *managerNativeConfig) (file *os.File, result error) {
	f, err := managerHoldFile(filepath.Join(c.components, "manifest.json"), c.manifest, 1<<20)
	if err != nil {
		return nil, err
	}
	ok := false
	defer func() {
		if !ok {
			if f.Close() != nil {
				result = managerWithCleanup(result, "manifest_handle_close_failed", true)
			}
		}
	}()
	raw, err := io.ReadAll(io.LimitReader(f, (1<<20)+1))
	var manifest localcomponents.Manifest
	if err != nil || len(raw) > 1<<20 || componentjson.Decode(raw, &manifest) != nil || manifest.Schema != 1 || len(manifest.Components) != 2 {
		return nil, managerFailure("manager_manifest_invalid")
	}
	seen := map[string]bool{}
	for _, component := range manifest.Components {
		if seen[component.Name] || component.Platform != "windows/amd64" || len(component.Files) == 0 || len(component.Files) > 101 {
			return nil, managerFailure("manager_component_identity_invalid")
		}
		seen[component.Name] = true
		var prefix string
		switch component.Name {
		case "source-streamer":
			prefix = "source/"
			if component.Profile != sourcestreamer.Profile || component.Version != managerFixtureSource || component.Entry != "source/source-streamer.exe" {
				return nil, managerFailure("frozen_source_role_mismatch")
			}
			c.sourceEntry = component.Entry
		case "source-preview-viewer":
			prefix = "viewer/"
			if component.Profile != Profile || component.Version != c.commit || component.Entry != "viewer/source-preview-viewer.exe" {
				return nil, managerFailure("current_viewer_role_mismatch")
			}
			c.viewerEntry = component.Entry
		default:
			return nil, managerFailure("unexpected_manager_component")
		}
		paths := map[string]bool{}
		entryFound := false
		for _, file := range component.Files {
			if !strings.HasPrefix(file.Path, prefix) || path.Dir(file.Path) != strings.TrimSuffix(prefix, "/") ||
				strings.ContainsAny(file.Path, "\\:\x00\r\n") || path.Clean(file.Path) != file.Path || paths[strings.ToLower(file.Path)] ||
				!managerValidHash(file.SHA256) || file.Size <= 0 || file.Size > 512<<20 {
				return nil, managerFailure("manager_component_member_invalid")
			}
			paths[strings.ToLower(file.Path)] = true
			if file.Path == component.Entry {
				if !file.Executable {
					return nil, managerFailure("manager_entry_not_executable")
				}
				entryFound = true
				if component.Name == "source-streamer" {
					c.sourceSHA = file.SHA256
				} else {
					c.viewerSHA = file.SHA256
				}
			}
		}
		if !entryFound {
			return nil, managerFailure("manager_entry_missing")
		}
	}
	if !seen["source-streamer"] || !seen["source-preview-viewer"] {
		return nil, managerFailure("manager_roles_missing")
	}
	ok = true
	return f, nil
}

type managerNativeObservation struct {
	source             *sourcestreamer.Session
	view               *viewerProcess
	viewerPath         string
	key, viewerKey     [32]byte
	id, viewerID       string
	keyID, viewerKeyID uint32
	grantValid         bool
	expires            time.Time
}

// Wrappers observe the exact arguments/results and return them unchanged. They
// cannot grant consent, change eligibility, substitute children, or fake frames.
func managerObserveLaunchers(m *Manager, current **managerNativeObservation, c managerNativeConfig, r *managerNativeReceipt) {
	start, view := m.deps.startStream, m.deps.startViewer
	m.deps.startStream = func(ctx context.Context, o localcomponents.Options, l sourcestreamer.Launch) (stream, error) {
		r.SourceStarts++
		ob := *current
		ob.key, ob.id, ob.keyID = sha256.Sum256([]byte(l.KeyB64)), l.SessionID, l.KeyID
		ob.grantValid = l.Owner == "local-preview" && l.Display == "desktop" && l.FFmpeg == c.fixture && l.CaptureConsent &&
			!l.InputConsent && l.PeerIP == "127.0.0.1" && l.VideoPort == 0 && l.AudioPort == 0 && l.Width == 128 && l.Height == 72 &&
			l.FPS == 30 && l.PixelFormat == "yuv420p" && l.PacketSize == 1024 && l.AudioMode == "silence" && l.MaxSeconds == 30
		child, err := start(ctx, o, l)
		if real, ok := child.(streamSession); ok {
			ob.source = real.Session
		}
		return child, err
	}
	m.deps.startViewer = func(ctx context.Context, path string, d descriptor) (viewer, error) {
		r.ViewerStarts++
		ob := *current
		ob.viewerPath, ob.expires = path, d.ExpiresAt
		ob.viewerKey, ob.viewerID, ob.viewerKeyID = sha256.Sum256([]byte(d.KeyB64)), d.SessionID, d.KeyID
		child, err := view(ctx, path, d)
		if real, ok := child.(*viewerProcess); ok {
			ob.view = real
		}
		return child, err
	}
}

func managerBoundedStop(m *Manager) error {
	done := make(chan error, 1)
	go func() { done <- m.Stop() }()
	return managerAwaitStop(done, 13*time.Second)
}

func managerStopResult(err error) error {
	if errors.Is(err, ErrCleanupUncertain) {
		return managerWithCleanup(managerFailure("manager_stop_failed"), "manager_stop_cleanup_uncertain", true)
	}
	if err != nil {
		return managerFailure("manager_stop_failed")
	}
	return nil
}

func managerAwaitStop(done <-chan error, timeout time.Duration) error {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	select {
	case err := <-done:
		return managerStopResult(err)
	case <-timer.C:
		// Prefer a completion concurrent with the deadline. Otherwise the
		// Stop worker remains unjoined: never report complete cleanup.
		select {
		case err := <-done:
			return managerStopResult(err)
		default:
			return managerWithCleanup(managerFailure("manager_stop_timeout"), "manager_stop_worker_not_joined", true)
		}
	}
}

func managerRetainViewer(pid uint32, expected string) (syscall.Handle, error) {
	if pid == 0 {
		return 0, managerFailure("viewer_pid_missing")
	}
	h, err := syscall.OpenProcess(syscall.SYNCHRONIZE|0x1000, false, pid)
	if err != nil {
		return 0, managerFailure("viewer_handle_failed")
	}
	var path [32768]uint16
	n := uint32(len(path))
	query := syscall.NewLazyDLL("kernel32.dll").NewProc("QueryFullProcessImageNameW")
	ok, _, _ := query.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&path[0])), uintptr(unsafe.Pointer(&n)))
	if ok == 0 || n == 0 || n >= uint32(len(path)) || !strings.EqualFold(filepath.Clean(syscall.UTF16ToString(path[:n])), filepath.Clean(expected)) {
		failure := managerFailure("viewer_identity_mismatch")
		if syscall.CloseHandle(h) != nil {
			failure = managerWithCleanup(failure, "viewer_process_handle_close_failed", true)
		}
		return 0, failure
	}
	return h, nil
}

func managerPortsReleased(ready sourcestreamer.Ready) (result error) {
	for _, address := range []string{ready.RTSPAddress, ready.ControlAddress} {
		host, _, err := net.SplitHostPort(address)
		if err != nil || host != "127.0.0.1" {
			return managerFailure("nonloopback_readiness")
		}
	}
	conn, err := net.DialTimeout("tcp4", ready.RTSPAddress, 400*time.Millisecond)
	if err == nil {
		result = managerFailure("rtsp_listener_remained")
		if conn.Close() != nil {
			result = managerWithCleanup(result, "probe_connection_close_failed", true)
		}
		return result
	}
	tcp, err := net.Listen("tcp4", ready.RTSPAddress)
	if err != nil {
		return managerFailure("rtsp_port_not_released")
	}
	defer func() {
		if tcp.Close() != nil {
			result = managerWithCleanup(result, "probe_tcp_listener_close_failed", true)
		}
	}()
	udp, err := net.ListenPacket("udp4", ready.ControlAddress)
	if err != nil {
		return managerFailure("control_port_not_released")
	}
	if udp.Close() != nil {
		return managerWithCleanup(nil, "probe_udp_listener_close_failed", true)
	}
	return nil
}

func managerNativeRunCase(m *Manager, c managerNativeConfig, approval Approval, ob *managerNativeObservation, r *managerNativeReceipt, cr *managerNativeCase) (result error) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	defer func() {
		result = managerMergeCleanup(result, managerBoundedStop(m))
		if result != nil {
			cr.Passed = false
		}
		var detail *managerCleanupError
		if errors.As(result, &detail) {
			cr.CleanupFailures = append([]string(nil), detail.codes...)
			cr.CleanupUncertain = detail.uncertain
			cr.NaturalJoined = false
		}
	}()
	s, err := m.Start(ctx, approval)
	if err != nil {
		return managerFailure("production_manager_start_failed")
	}
	r.ActualManagerStarted = true
	if ob.source == nil || ob.view == nil || !ob.grantValid || ob.id == "" || ob.id != s.ID() || ob.id != ob.viewerID || ob.key != ob.viewerKey || ob.keyID != ob.viewerKeyID {
		return managerFailure("production_launch_observation_failed")
	}
	// Hash and retain the actual staged entries, not their original inputs.
	// This is file identity evidence, not an independent loaded-DLL/OS-module
	// observation; the separate public viewer gate supplies that latter proof.
	sourcePath := localcomponents.PreparedPath(approval.Components.StateDir, "source-streamer")
	viewerPath := localcomponents.PreparedPath(approval.Components.StateDir, "source-preview-viewer")
	if sourcePath != filepath.Join(approval.Components.StateDir, "local-components", "source-streamer", filepath.FromSlash(c.sourceEntry)) ||
		viewerPath != filepath.Join(approval.Components.StateDir, "local-components", "source-preview-viewer", filepath.FromSlash(c.viewerEntry)) || viewerPath != ob.viewerPath {
		return managerFailure("staged_entry_identity_mismatch")
	}
	stagedSource, err := managerHoldFile(sourcePath, c.sourceSHA, 64<<20)
	if err != nil {
		return err
	}
	defer func() {
		if stagedSource.Close() != nil {
			result = managerWithCleanup(result, "staged_source_handle_close_failed", true)
		}
	}()
	stagedViewer, err := managerHoldFile(viewerPath, c.viewerSHA, 64<<20)
	if err != nil {
		return err
	}
	defer func() {
		if stagedViewer.Close() != nil {
			result = managerWithCleanup(result, "staged_viewer_handle_close_failed", true)
		}
	}()
	cr.StagedSourceSHA, cr.StagedViewerSHA, cr.StagedVerified = c.sourceSHA, c.viewerSHA, true
	pid := ob.view.pid()
	handle, err := managerRetainViewer(pid, ob.viewerPath)
	if err != nil {
		return err
	}
	defer func() {
		if syscall.CloseHandle(handle) != nil {
			result = managerWithCleanup(result, "viewer_process_handle_close_failed", true)
		}
	}()
	starts, viewers := r.SourceStarts, r.ViewerStarts
	if extra, err := m.Start(ctx, approval); err == nil || extra != nil || starts != r.SourceStarts || viewers != r.ViewerStarts {
		return managerFailure("overlapping_launch_accepted")
	}
	cr.OverlapRejected = true
	select {
	case <-ob.view.FirstFrame():
		cr.FirstFrame = true
	case <-s.Done():
		return managerFailure("session_ended_before_frame")
	case <-time.After(7 * time.Second):
		return managerFailure("first_frame_timeout")
	}
	deadline := time.Now().Add(7 * time.Second)
	var hwnd uintptr
	lastColor := 0
	for time.Now().Before(deadline) {
		select {
		case <-s.Done():
			return managerFailure("session_ended_before_pixels")
		default:
		}
		current, _, err := managerOwnedWindowSnapshot(pid)
		if err != nil {
			return err
		}
		if current != 0 {
			if hwnd != 0 && current != hwnd {
				return managerFailure("owned_window_identity_changed")
			}
			hwnd = current
			color, err := managerBoundedPixels(hwnd, pid)
			if err != nil {
				return err
			}
			cr.Samples++
			if color != 0 {
				if lastColor != 0 && lastColor != color {
					cr.Transitions++
				}
				lastColor = color
				cr.Blue = cr.Blue || color == 1
				cr.Orange = cr.Orange || color == 2
			}
		}
		cr.Viewing = s.State() == "viewing"
		if cr.Blue && cr.Orange && cr.Transitions >= 2 && cr.Viewing {
			break
		}
		time.Sleep(100 * time.Millisecond)
	}
	if hwnd == 0 || !cr.Blue || !cr.Orange || cr.Transitions < 2 || !cr.Viewing {
		return managerFailure("owned_changing_pixels_unverified")
	}
	if time.Until(ob.expires) < 8*time.Second {
		return managerFailure("stop_action_expiry_margin")
	}
	switch cr.Name {
	case "manager_stop":
		if err := managerBoundedStop(m); err != nil {
			return err
		}
	case "parent_cancel":
		cancel()
	default:
		return managerFailure("unknown_manager_case")
	}
	select {
	case <-s.Done():
	case <-time.After(13 * time.Second):
		return managerFailure("manager_session_join_timeout")
	}
	if sessionErr := s.Wait(); sessionErr != nil || s.State() != "stopped" {
		failure := managerFailure("manager_session_not_natural")
		if errors.Is(sessionErr, ErrCleanupUncertain) {
			failure = managerWithCleanup(failure, "manager_session_cleanup_uncertain", true)
		}
		return failure
	}
	for _, done := range []<-chan struct{}{ob.source.Done(), ob.view.Done(), ob.view.writeDone} {
		select {
		case <-done:
		default:
			return managerFailure("production_child_not_joined")
		}
	}
	if ob.source.Wait() != nil || ob.view.Wait() != nil {
		return managerFailure("production_child_not_natural")
	}
	terminal, present := ob.source.Terminal()
	if !present || terminal.Reason != "completed" || terminal.FailureCode != "" || terminal.Stats == nil ||
		terminal.Stats.VideoFrames == 0 || terminal.Stats.VideoFrames > 900 || terminal.Stats.VideoPackets < terminal.Stats.VideoFrames ||
		terminal.Stats.AudioPackets == 0 || terminal.Stats.AudioPackets > 6001 {
		return managerFailure("source_media_terminal_unverified")
	}
	cr.VideoFrames, cr.AudioPackets = terminal.Stats.VideoFrames, terminal.Stats.AudioPackets
	cr.NaturalJoined = true
	status, err := syscall.WaitForSingleObject(handle, 0)
	if err != nil || status != syscall.WAIT_OBJECT_0 {
		return managerFailure("viewer_process_not_exited")
	}
	stillWindow, _, _ := managerIsWindow.Call(hwnd)
	current, snapshot, err := managerOwnedWindowSnapshot(pid)
	if err != nil || stillWindow != 0 || current != 0 || snapshot.Owned != 0 {
		return managerFailure("owned_window_remained")
	}
	cr.WindowGone = true
	if err := managerPortsReleased(ob.source.Ready); err != nil {
		return err
	}
	cr.ListenersClosed = true
	m.mu.Lock()
	available := m.active == nil && !m.cleanupUncertain
	m.mu.Unlock()
	if !available {
		return managerFailure("manager_reservation_not_released")
	}
	cr.Passed = true
	return nil
}

func TestWindowsProductionManagerGeneratedAcceptance(t *testing.T) {
	c, err := managerNativeConfigFromEnvironment()
	if err != nil {
		t.Fatal(err)
	}
	r := managerNativeReceipt{Schema: 1, Platform: "windows/amd64", Commit: c.commit, SourceCommit: managerFixtureSource, ManifestSHA: c.manifest, FixtureSHA: c.fixtureSHA,
		GeneratedCaptureOnly: true, FreshKeys: true, FreshSessions: true, FreshKeyIDs: true, Cases: []managerNativeCase{}}
	defer func() {
		raw, err := json.MarshalIndent(r, "", "  ")
		if err != nil || len(raw) > 16384 {
			t.Error("manager_receipt_encoding_failed")
			return
		}
		f, err := os.OpenFile(filepath.Join(c.output, "manager.json"), os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0600)
		if err != nil {
			t.Error("manager_receipt_create_failed")
			return
		}
		_, writeErr := f.Write(append(raw, '\n'))
		syncErr, closeErr := f.Sync(), f.Close()
		if writeErr != nil || syncErr != nil || closeErr != nil {
			t.Error("manager_receipt_write_failed")
		}
	}()
	fail := func(code string) { managerRecordFailure(&r, managerFailure(code)); t.Fatal(code) }
	r.Eligibility = previeweligibility.Observe()
	if !r.Eligibility.Eligible() {
		fail("native_manager_eligibility_failed")
	}
	manifest, err := managerHoldManifest(&c)
	if err != nil {
		managerRecordFailure(&r, err)
		fail(err.Error())
	}
	defer func() {
		if manifest.Close() != nil {
			managerRecordFailure(&r, managerWithCleanup(nil, "manifest_handle_close_failed", true))
			t.Error("manifest_handle_close_failed")
		}
	}()
	r.ManifestVerified = true
	held, err := managerHoldFixture(c)
	if err != nil {
		managerRecordFailure(&r, err)
		fail(err.Error())
	}
	defer func() {
		for _, f := range held {
			if f.Close() != nil {
				managerRecordFailure(&r, managerWithCleanup(nil, "fixture_handle_close_failed", true))
				t.Error("fixture_handle_close_failed")
			}
		}
	}()
	r.FixturePinned = true
	// Only manager.json belongs in the artifact directory. Staged executables
	// and DLLs remain in the test's private temporary tree and are never output.
	state := t.TempDir()
	approval := Approval{Components: localcomponents.Options{Directory: c.components, ManifestSHA256: c.manifest, StateDir: state}, Display: "desktop", FFmpeg: c.fixture, CaptureConsent: true, VideoProfile: "transport-128"}
	m := New()
	defer func() {
		if err := managerBoundedStop(m); err != nil {
			managerRecordFailure(&r, err)
			t.Error(err.Error())
		}
	}()
	if !m.deps.permitted() {
		fail("production_manager_gate_disabled")
	}
	current := &managerNativeObservation{}
	managerObserveLaunchers(m, &current, c, &r)
	withoutConsent := approval
	withoutConsent.CaptureConsent = false
	if s, err := m.Start(context.Background(), withoutConsent); err == nil || s != nil || r.SourceStarts != 0 || r.ViewerStarts != 0 {
		fail("missing_consent_launched")
	}
	r.ConsentRejected = true
	keys, ids, keyIDs := map[[32]byte]bool{}, map[string]bool{}, map[uint32]bool{}
	for _, name := range []string{"manager_stop", "parent_cancel"} {
		current = &managerNativeObservation{}
		r.Cases = append(r.Cases, managerNativeCase{Name: name})
		if err := managerNativeRunCase(m, c, approval, current, &r, &r.Cases[len(r.Cases)-1]); err != nil {
			managerRecordFailure(&r, err)
			fail(err.Error())
		}
		r.FreshKeys = r.FreshKeys && !keys[current.key]
		r.FreshSessions = r.FreshSessions && !ids[current.id]
		r.FreshKeyIDs = r.FreshKeyIDs && !keyIDs[current.keyID]
		keys[current.key], ids[current.id], keyIDs[current.keyID] = true, true, true
	}
	if !r.FreshKeys || !r.FreshSessions || !r.FreshKeyIDs || r.SourceStarts != 2 || r.ViewerStarts != 2 {
		fail("fresh_restart_unverified")
	}
	r.Passed = true
}

// These native helper tests use only generated text files and pure decisions;
// they never invoke a preview, process launcher, token-changing or input API.
func TestWindowsManagerAcceptancePinsAndRoles(t *testing.T) {
	root := t.TempDir()
	fixture := filepath.Join(root, "ffmpeg-fixture.exe")
	data := []byte("generated pin test, never executable")
	if os.WriteFile(fixture, data, 0600) != nil {
		t.Fatal("fixture_write_failed")
	}
	digest := sha256.Sum256(data)
	pin := hex.EncodeToString(digest[:])
	if f, err := managerHoldFile(fixture, strings.Repeat("0", 64), 1024); err == nil || f != nil {
		t.Fatal("wrong_hash_accepted")
	}
	f, err := managerHoldFile(fixture, pin, 1024)
	if err != nil {
		t.Fatal("correct_pin_rejected")
	}
	if os.WriteFile(fixture, []byte("replacement"), 0600) == nil {
		_ = f.Close()
		t.Fatal("held_fixture_writable")
	}
	if f.Close() != nil {
		t.Fatal("held_fixture_close_failed")
	}
	commit := strings.Repeat("a", 40)
	manifest := localcomponents.Manifest{Schema: 1, Components: []localcomponents.Component{
		{Name: "source-streamer", Platform: "windows/amd64", Version: managerFixtureSource, Profile: sourcestreamer.Profile, Entry: "source/source-streamer.exe", Files: []localcomponents.File{{Path: "source/source-streamer.exe", SHA256: pin, Size: int64(len(data)), Executable: true}}},
		{Name: "source-preview-viewer", Platform: "windows/amd64", Version: commit, Profile: Profile, Entry: "viewer/source-preview-viewer.exe", Files: []localcomponents.File{{Path: "viewer/source-preview-viewer.exe", SHA256: pin, Size: int64(len(data)), Executable: true}}},
	}}
	for _, tc := range []struct {
		name   string
		change func(*localcomponents.Manifest)
		valid  bool
	}{
		{"valid", func(*localcomponents.Manifest) {}, true},
		{"source_version", func(m *localcomponents.Manifest) { m.Components[0].Version = "other" }, false},
		{"viewer_version", func(m *localcomponents.Manifest) { m.Components[1].Version = "other" }, false},
		{"source_profile", func(m *localcomponents.Manifest) { m.Components[0].Profile = Profile }, false},
		{"viewer_profile", func(m *localcomponents.Manifest) { m.Components[1].Profile = sourcestreamer.Profile }, false},
		{"wrong_platform", func(m *localcomponents.Manifest) { m.Components[0].Platform = "linux/amd64" }, false},
		{"wrong_entry", func(m *localcomponents.Manifest) { m.Components[1].Entry = "source/source-streamer.exe" }, false},
		{"missing_role", func(m *localcomponents.Manifest) { m.Components = m.Components[:1] }, false},
		{"duplicate_role", func(m *localcomponents.Manifest) { m.Components[1] = m.Components[0] }, false},
	} {
		t.Run(tc.name, func(t *testing.T) {
			raw, _ := json.Marshal(manifest)
			var changed localcomponents.Manifest
			if json.Unmarshal(raw, &changed) != nil {
				t.Fatal("test_manifest_copy_failed")
			}
			tc.change(&changed)
			raw, _ = json.Marshal(changed)
			hash := sha256.Sum256(raw)
			if os.WriteFile(filepath.Join(root, "manifest.json"), raw, 0600) != nil {
				t.Fatal("test_manifest_write_failed")
			}
			c := managerNativeConfig{components: root, manifest: hex.EncodeToString(hash[:]), commit: commit}
			held, err := managerHoldManifest(&c)
			if held != nil {
				defer held.Close()
			}
			if (err == nil) != tc.valid {
				t.Fatal("manifest_role_verdict_wrong")
			}
			if tc.valid && (c.sourceSHA != pin || c.viewerSHA != pin) {
				t.Fatal("manifest_entry_pins_missing")
			}
		})
	}
}

func TestWindowsManagerAcceptancePixelAndPathDecisions(t *testing.T) {
	if managerPixelColor(180, 75, 22) != 1 || managerPixelColor(49, 99, 219) != 2 || managerPixelColor(0, 0, 0) != 0 {
		t.Fatal("owned_color_classifier_changed")
	}
	for _, p := range []string{`C:\owned\fixture.exe`, `D:/owned/fixture.exe`} {
		if !managerLocalPath(p) {
			t.Fatal("local_drive_rejected")
		}
	}
	for _, p := range []string{`\\server\share\fixture.exe`, `C:fixture.exe`, `fixture.exe`, `C:\owned\fixture.exe:stream`, "C:\\owned\\fixture.exe\n"} {
		if managerLocalPath(p) {
			t.Fatal("nonlocal_path_accepted")
		}
	}
}

func TestWindowsManagerAcceptanceCleanupAccounting(t *testing.T) {
	primary := managerFailure("owned_changing_pixels_unverified")
	result := primary
	codes := []string{"staged_source_handle_close_failed", "staged_viewer_handle_close_failed", "viewer_process_handle_close_failed", "probe_tcp_listener_close_failed", "probe_udp_listener_close_failed"}
	for _, code := range codes {
		result = managerWithCleanup(result, code, true)
		if result.Error() != primary.Error() || !errors.Is(result, primary) {
			t.Fatal("cleanup_replaced_primary_failure")
		}
	}
	// A timeout is explicitly unjoined and uncertain. No real Stop worker or
	// child is created by this deterministic deadline test.
	unreturned := make(chan error)
	timeout := managerAwaitStop(unreturned, 0)
	var detail *managerCleanupError
	if timeout == nil || timeout.Error() != "manager_stop_timeout" || !errors.As(timeout, &detail) || !detail.uncertain || len(detail.codes) != 1 || detail.codes[0] != "manager_stop_worker_not_joined" {
		t.Fatal("stop_timeout_claimed_join")
	}
	result = managerMergeCleanup(result, timeout)
	r := managerNativeReceipt{Passed: true}
	managerRecordFailure(&r, result)
	if r.Passed || r.Failure != primary.Error() || !r.CleanupUncertain || len(r.CleanupFailures) != len(codes)+2 {
		t.Fatal("cleanup_receipt_lost_primary_or_uncertainty")
	}
	managerRecordFailure(&r, managerFailure("manager_stop_failed"))
	if r.Failure != primary.Error() || r.CleanupFailures[len(r.CleanupFailures)-1] != "manager_stop_failed" {
		t.Fatal("late_cleanup_replaced_primary")
	}
	finished := make(chan error, 1)
	finished <- nil
	if managerAwaitStop(finished, 0) != nil {
		t.Fatal("completed_stop_lost_at_deadline")
	}
	// Neither a later successful cleanup nor duplicate observations erase the
	// earlier missing join or inflate the bounded receipt.
	managerRecordFailure(&r, managerMergeCleanup(result, nil))
	if r.Passed || !r.CleanupUncertain || r.Failure != primary.Error() || len(r.CleanupFailures) != len(codes)+3 {
		t.Fatal("cleanup_uncertainty_not_sticky")
	}
	cleanFailure := managerWithCleanup(nil, "probe_tcp_listener_close_failed", true)
	if cleanFailure == nil || cleanFailure.Error() != "manager_cleanup_failed" {
		t.Fatal("close_failure_accepted_as_success")
	}
	if got := managerStopResult(errors.New("private_backend_detail")); got == nil || got.Error() != "manager_stop_failed" {
		t.Fatal("stop_failure_not_sanitized")
	}
	if got := managerStopResult(ErrCleanupUncertain); !errors.As(got, &detail) || !detail.uncertain {
		t.Fatal("production_cleanup_uncertainty_lost")
	}
}
