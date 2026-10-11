//go:build windows

package main

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
)

type signatureJobAccounting struct {
	UserTime, KernelTime, PeriodUserTime, PeriodKernelTime int64
	PageFaults, Total, Active, Terminated                  uint32
}

func signatureAccounting(j *job) (signatureJobAccounting, error) {
	var a signatureJobAccounting
	ok, _, _ := queryJob.Call(uintptr(j.handle), 1, uintptr(unsafe.Pointer(&a)), unsafe.Sizeof(a), 0)
	if ok == 0 || a.Total > 2 || a.Active > a.Total || a.Terminated != 0 {
		return a, failure("signature_job_accounting_failed")
	}
	return a, nil
}

type signatureConsoleOwner struct {
	file                                   *os.File
	path, hash                             string
	handle                                 syscall.Handle
	rootHandle                             syscall.Handle
	policy                                 signatureOwnerPolicy
	beforeTotal, beforeActive, beforeCount uint32
	resultObserved, rootZero, hostZero     bool
}

func signatureSystemDirectory(root string) (string, error) {
	var buffer [32768]uint16
	n, _, _ := kernel32.NewProc("GetSystemDirectoryW").Call(uintptr(unsafe.Pointer(&buffer[0])), uintptr(len(buffer)))
	if n == 0 || n >= uintptr(len(buffer)) {
		return "", failure("signature_system_directory_failed")
	}
	path := graphicsPath(syscall.UTF16ToString(buffer[:n]))
	if !strings.EqualFold(path, graphicsPath(filepath.Join(root, "System32"))) {
		return "", failure("signature_system_directory_mismatch")
	}
	return path, nil
}
func prepareSignatureConsole(root string) (*signatureConsoleOwner, error) {
	system, err := signatureSystemDirectory(root)
	if err != nil {
		return nil, err
	}
	path := filepath.Join(system, "conhost.exe")
	f, err := lockFile(path)
	if err != nil {
		return nil, failure("signature_host_lock_failed")
	}
	final, err := finalGraphicsPath(f)
	if err != nil || !strings.EqualFold(final, graphicsPath(path)) {
		f.Close()
		return nil, failure("signature_host_final_path_failed")
	}
	hash, err := fileSHA(f, 64<<20)
	if err != nil {
		f.Close()
		return nil, failure("signature_host_hash_failed")
	}
	return &signatureConsoleOwner{file: f, path: final, hash: hash}, nil
}
func (o *signatureConsoleOwner) close() {
	if o.rootHandle != 0 {
		syscall.CloseHandle(o.rootHandle)
	}
	if o.handle != 0 {
		syscall.CloseHandle(o.handle)
	}
	o.file.Close()
}
func (o *signatureConsoleOwner) observe(j *job, p *child, ids []uint32) error {
	if err := signatureUniqueInventory(ids); err != nil {
		return err
	}
	for _, id := range ids {
		if id == p.pid {
			if !j.contains(p.handle) {
				return failure("signature_root_membership_failed")
			}
			continue
		}
		if id == o.policy.host {
			if !j.contains(o.handle) {
				return failure("signature_host_membership_failed")
			}
			continue
		}
		path, h, err := processPath(id)
		if err != nil {
			return failure("signature_member_inspection_failed")
		}
		owned := j.contains(h)
		samePath := strings.EqualFold(graphicsPath(path), o.path)
		sameHash := false
		if samePath {
			info, e := os.Stat(path)
			held, he := o.file.Stat()
			if e == nil && he == nil && os.SameFile(info, held) {
				got, e := fileSHA(o.file, 64<<20)
				sameHash = e == nil && got == o.hash
			}
		}
		err = o.policy.admit(id, samePath, sameHash, owned)
		if err != nil {
			syscall.CloseHandle(h)
			return err
		}
		o.handle = h
	}
	return nil
}
func signatureProcessZero(h syscall.Handle) (bool, error) {
	state, err := syscall.WaitForSingleObject(h, 0)
	if err != nil {
		return false, failure("signature_process_wait_failed")
	}
	if state == syscall.WAIT_TIMEOUT {
		return false, nil
	}
	if state != syscall.WAIT_OBJECT_0 {
		return false, failure("signature_process_wait_failed")
	}
	var code uint32
	if syscall.GetExitCodeProcess(h, &code) != nil || code != 0 {
		return false, failure("signature_process_nonzero_exit")
	}
	return true, nil
}

// Copy only observations made before the deadline, never forced-cleanup exits.
func (o *signatureConsoleOwner) recordTimeout(r *graphicsOSInspection) {
	r.TimedOut = true
	r.ResultObservedAtTimeout = o.resultObserved
	r.RootZeroAtTimeout = o.rootZero
	r.HostZeroAtTimeout = o.hostZero
}

// Observe and retain both identities before accepting the single protocol line.
// Lifetime accounting also rejects an unknown descendant too short-lived to
// appear in a snapshot. No member is ever killed on a successful return.
func (o *signatureConsoleOwner) collect(ctx context.Context, j *job, p *child, r *graphicsOSInspection) (raw []byte, result error) {
	defer func() {
		if signatureBudgetErr(ctx) != nil {
			o.recordTimeout(r)
			result = failure("signature_owner_timeout")
		}
	}()
	o.policy.root = p.pid
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	packets := p.packets
	var protocolErr error
	for {
		if signatureBudgetErr(ctx) != nil {
			return raw, failure("signature_owner_timeout")
		}
		rootZero, e := signatureProcessZero(p.handle)
		if e != nil {
			return raw, e
		}
		if signatureBudgetErr(ctx) != nil {
			return raw, failure("signature_owner_timeout")
		}
		o.rootZero = rootZero
		o.policy.rootExited = rootZero
		ids, invErr := j.pids()
		// Incomplete enumeration is never empty. It may retire only after both
		// retained identities have signaled, using the existing strict deadline.
		if invErr != nil {
			if invErr != errIncompleteInventory || !rootZero || o.handle == 0 {
				return raw, failure("signature_inventory_failed")
			}
			if e = o.observe(j, p, ids); e != nil {
				return raw, e
			}
			hostZero, e := signatureProcessZero(o.handle)
			if e != nil || !hostZero {
				return raw, failure("signature_inventory_failed")
			}
			if signatureBudgetErr(ctx) != nil {
				return raw, failure("signature_owner_timeout")
			}
			o.rootZero, o.hostZero = rootZero, hostZero
			if e = waitSignatureRetiredInventory(ctx, j.pids, map[uint32]bool{p.pid: true, o.policy.host: true}, time.Second); e != nil {
				return raw, e
			}
			ids = nil
		}
		if e = o.observe(j, p, ids); e != nil {
			return raw, e
		}
		a, e := signatureAccounting(j)
		if e != nil {
			return raw, e
		}
		hostZero := false
		if o.handle != 0 {
			hostZero, e = signatureProcessZero(o.handle)
			if e != nil {
				return raw, e
			}
		}
		if signatureBudgetErr(ctx) != nil {
			return raw, failure("signature_owner_timeout")
		}
		o.rootZero, o.hostZero = rootZero, hostZero
		if rootZero && hostZero && len(ids) == 0 {
			if e = o.policy.finish(a.Total, a.Active, true, rootZero, hostZero, j.closed.Load()); e != nil {
				return raw, e
			}
			if e = p.signatureWaitContext(ctx); e != nil {
				return raw, e
			}
			if raw == nil && protocolErr == nil {
				raw, protocolErr = p.signatureNextContext(ctx)
				o.resultObserved = raw != nil
			}
			if e = p.signatureFinishProtocolContext(ctx); e != nil {
				return raw, e
			}
			if signatureBudgetErr(ctx) != nil {
				return raw, failure("signature_owner_timeout")
			}
			r.NaturalCleanup = true
			r.ConsoleHostSHA = o.hash
			r.ConsoleHostVerified = true
			r.TotalOwnedProcesses = a.Total
			if protocolErr != nil {
				return raw, protocolErr
			}
			return raw, nil
		}
		select {
		case v, ok := <-packets:
			if signatureBudgetErr(ctx) != nil {
				clear(v.line)
				return raw, failure("signature_owner_timeout")
			}
			o.resultObserved = ok && v.err == nil
			packets = nil
			o.policy.frozen = true
			if o.policy.host == 0 {
				return raw, failure("signature_host_not_observed_before_result")
			}
			if !ok {
				protocolErr = failure("protocol_ended_early")
			} else {
				raw = v.line
				protocolErr = v.err
			}
		case <-ticker.C:
		case <-ctx.Done():
			return raw, failure("signature_owner_timeout")
		}
	}
}

// Resume only the locked OS PowerShell root. The OS creates conhost afterward;
// the immutable script emits a literal and waits for private stdin before any
// file query. startup freezes exact host admission before a request is sent.
func (o *signatureConsoleOwner) beforeResume(j *job, pid uint32) error {
	path, h, err := processPath(pid)
	if err != nil {
		return err
	}
	o.rootHandle = h
	if !strings.EqualFold(graphicsPath(path), filepath.Join(filepath.Dir(o.path), "WindowsPowerShell", "v1.0", "powershell.exe")) {
		return failure("signature_root_identity_failed")
	}
	if !j.contains(h) {
		return failure("signature_root_membership_failed")
	}
	o.policy.root = pid
	ids, err := j.pids()
	if err != nil {
		return err
	}
	o.beforeCount = uint32(len(ids))
	a, err := signatureAccounting(j)
	if err != nil {
		return err
	}
	o.beforeTotal, o.beforeActive = a.Total, a.Active
	if len(ids) != 1 || ids[0] != pid || a.Total != 1 || a.Active != 1 {
		return failure("signature_suspended_root_incomplete")
	}
	return nil
}
func (o *signatureConsoleOwner) startup(ctx context.Context, j *job, p *child, r *graphicsOSInspection) (result error) {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	defer func() {
		if signatureBudgetErr(ctx) != nil {
			o.recordTimeout(r)
			result = failure("signature_startup_timeout")
		}
	}()
	ticker := time.NewTicker(5 * time.Millisecond)
	defer ticker.Stop()
	for {
		if signatureBudgetErr(ctx) != nil {
			return failure("signature_startup_timeout")
		}
		zero, err := signatureProcessZero(p.handle)
		if err != nil {
			return err
		}
		if zero {
			return failure("signature_startup_early_exit")
		}
		ids, err := j.pids()
		if err != nil {
			return failure("signature_startup_inventory_failed")
		}
		if err = o.observe(j, p, ids); err != nil {
			return err
		}
		if _, err = signatureAccounting(j); err != nil {
			return err
		}
		select {
		case packet, ok := <-p.packets:
			if !ok || packet.err != nil {
				return failure("signature_startup_marker_missing")
			}
			defer clear(packet.line)
			// Revalidate the exact, still-live graph when accepting the literal.
			ids, err = j.pids()
			if err != nil {
				return failure("signature_startup_inventory_failed")
			}
			if err = o.observe(j, p, ids); err != nil {
				return err
			}
			a, err := signatureAccounting(j)
			if err != nil {
				return err
			}
			if !signatureExactInitialSet(ids, p.pid, o.policy.host) {
				return failure("signature_startup_graph_incomplete")
			}
			if zero, err = signatureProcessZero(p.handle); err != nil || zero {
				return failure("signature_startup_early_exit")
			}
			if err = o.policy.freezeStartup(packet.line, a.Total, a.Active); err != nil {
				return err
			}
			if zero, err = signatureProcessZero(o.handle); err != nil || zero {
				return failure("signature_startup_host_exited")
			}
			// No request was sent yet. Any queued extra output is not a query result.
			select {
			case extra, ok := <-p.packets:
				if ok {
					clear(extra.line)
				}
				return failure("signature_startup_early_output")
			default:
			}
			if signatureBudgetErr(ctx) != nil {
				return failure("signature_startup_timeout")
			}
			r.StartupHandshakeVerified = true
			return nil
		case <-ticker.C:
		case <-ctx.Done():
			return failure("signature_startup_timeout")
		}
	}
}
