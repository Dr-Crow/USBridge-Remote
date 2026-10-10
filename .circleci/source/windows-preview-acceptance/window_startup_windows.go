//go:build windows

package main

import (
	"net"
	"os"
	"runtime"
	"strings"
	"time"
)

// Only the exact normal viewer entry is launched. The owned listener never
// implements RTSP or delivers media; it holds connection setup until shutdown.
func executeWindowStartup(c config, r *windowStartupReceipt) (result error) {
	if runtime.GOARCH != "amd64" {
		return failure("native_windows_amd64_required")
	}
	if _, err := os.Lstat(c.Work); !os.IsNotExist(err) {
		return failure("work_directory_must_be_new")
	}
	if os.Mkdir(c.Work, 0700) != nil {
		return failure("work_directory_failed")
	}
	root := os.Getenv("SystemRoot")
	if !drivePath.MatchString(root) || strings.ContainsAny(root, ";\x00\r\n") {
		return failure("invalid_system_root")
	}
	pins := new(pins)
	defer pins.close()
	if _, err := pins.check(c.Viewer, c.ViewerSHA, 512<<20); err != nil {
		return err
	}
	var graphics map[string]string
	if c.GraphicsStaging != "" {
		var err error
		graphics, err = pinGraphicsStaging(pins, c.Viewer, c.GraphicsStaging, c.GraphicsStagingSHA)
		if err != nil {
			return err
		}
	}
	listener, err := net.Listen("tcp4", "127.0.0.1:0")
	if err != nil {
		return failure("owned_listener_failed")
	}
	defer listener.Close()
	j, err := newJob()
	if err != nil {
		return err
	}
	defer j.close()
	watchdog := time.AfterFunc(15*time.Second, j.close)
	defer watchdog.Stop()
	p, err := j.start(c.Viewer, []string{"--source-preview-stdin"}, childEnvironment(root, c.Work), c.Work)
	if err != nil {
		return err
	}
	defer func() {
		if ids, err := j.pids(); j.closed.Load() || err != nil || len(ids) != 0 {
			r.SafetyKill = true
			if result == nil {
				result = failure("safety_cleanup_required")
			}
			r.NaturalCleanup, r.JobEmpty = false, false
		}
		if p.alive() {
			r.SafetyKill = true
			j.close()
			_ = p.wait(3 * time.Second)
		}
		p.close()
	}()
	id, launch, descriptor, err := privatePayloads("")
	delete(launch, "key_b64")
	if err != nil {
		return err
	}
	defer delete(descriptor, "key_b64")
	descriptor["rtsp_url"] = "rtspenc://" + listener.Addr().String()
	descriptor["expires_at"] = time.Now().UTC().Add(20 * time.Second)
	if err := writePrivate(p.stdin, descriptor); err != nil {
		return err
	}
	delete(descriptor, "key_b64")
	deadline := time.Now().Add(5 * time.Second)
	terminal := false
	for time.Now().Before(deadline) {
		select {
		case packet, ok := <-p.packets:
			if !ok || packet.err != nil {
				return failure("startup_protocol_ended")
			}
			code, err := startupTerminal(packet.line, id)
			clear(packet.line)
			if err != nil {
				return err
			}
			r.ViewerFailure = code
			terminal = true
		default:
		}
		if terminal {
			break
		}
		if !p.alive() {
			time.Sleep(25 * time.Millisecond)
			continue
		}
		ids, err := j.pids()
		if err != nil {
			return err
		}
		for _, pid := range ids {
			if pid != p.pid {
				return failure("unexpected_owned_process")
			}
		}
		_, snap, err := ownedWindowSnapshot(p.pid)
		if err != nil {
			return err
		}
		r.OwnedWindows = max(r.OwnedWindows, snap.Owned)
		r.VisibleWindows = max(r.VisibleWindows, snap.Visible)
		r.TitleMatches = max(r.TitleMatches, snap.TitleMatches)
		if snap.Visible > 0 && snap.TitleMatches > 0 {
			window, err := ownedWindow(p.pid)
			if err != nil || window == 0 {
				return failure("owned_window_identity_failed")
			}
			r.WindowVerified = true
			if graphics != nil {
				r.VerifiedOSModules = map[string]graphicsOSInspection{}
				r.GraphicsModules, err = graphicsModules(p.pid, c.Viewer, root, graphics, pins, r.VerifiedOSModules)
				if err != nil {
					return err
				}
			}
			break
		}
		time.Sleep(25 * time.Millisecond)
	}
	_ = p.stdin.Close()
	// Closing this owned unresponsive endpoint releases any pending connect.
	_ = listener.Close()
	if !terminal {
		raw, err := p.next(6 * time.Second)
		if err != nil {
			return err
		}
		// The inert endpoint can fail while EOF cancels the renderer. Both are
		// expected here; the real media gate separately demands completed.
		valid := parseViewer(raw, id, "stopped", "completed") == nil || parseViewer(raw, id, "stopped", "failed") == nil
		clear(raw)
		if !valid {
			return failure("invalid_startup_terminal")
		}
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
	r.JobEmpty, r.NaturalCleanup = true, true
	if !r.WindowVerified {
		return failure("actual_viewer_window_unavailable")
	}
	return nil
}
