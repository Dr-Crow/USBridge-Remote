//go:build windows && signatureprobe

package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"unsafe"
)

// Run once in the fast source-free native job, before any Fyne build. This uses
// an existing OS file only; it does not load GDI+, open windows or accept modules.
func TestWindowsSignatureVerifierSystemFile(t *testing.T) {
	if unsafe.Sizeof(signatureJobAccounting{}) != 48 || unsafe.Offsetof(signatureJobAccounting{}.Total) != 36 {
		t.Fatal("signature_accounting_layout")
	}
	out, root := os.Getenv("WINDOWS_SIGNATURE_PROBE_RECEIPT"), os.Getenv("SystemRoot")
	if !drivePath.MatchString(out) || !drivePath.MatchString(root) {
		t.Fatal("os_signature_probe_configuration_missing")
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatal("os_signature_receipt_must_be_new")
	}
	r := graphicsOSInspection{}
	inspect := func() error {
		path := filepath.Join(root, "System32", "kernel32.dll")
		f, err := lockFile(path)
		if err != nil {
			return err
		}
		defer f.Close()
		final, err := finalGraphicsPath(f)
		if err != nil {
			return err
		}
		if !strings.EqualFold(final, graphicsPath(path)) {
			return failure("os_module_final_path_mismatch")
		}
		r.FinalPathMatches = true
		r.FileSHA, err = fileSHA(f, 64<<20)
		if err != nil {
			return err
		}
		return runGraphicsSignature(final, root, &r)
	}
	if err := inspect(); err != nil {
		r.Failure = err.Error()
	}
	passed := r.Failure == "" && r.CleanupJoined && r.SuspendedGraphVerified && !r.SafetyJobClosed && r.ConsoleHostVerified && r.TotalOwnedProcesses == 2 && r.NaturalCleanup && r.Signature != nil && r.Signature.Status == 0 && r.Signature.OSBinary
	receipt := struct {
		Schema     int                  `json:"schema_version"`
		Commit     string               `json:"commit"`
		Passed     bool                 `json:"passed"`
		Inspection graphicsOSInspection `json:"inspection"`
	}{1, os.Getenv("CIRCLE_SHA1"), passed, r}
	if !commitPattern.MatchString(receipt.Commit) {
		t.Fatal("os_signature_probe_commit_missing")
	}
	raw, err := json.MarshalIndent(receipt, "", "  ")
	if err != nil || writeReceipt(out, append(raw, '\n')) != nil {
		t.Fatal("os_signature_probe_receipt_failed")
	}
	if !passed {
		t.Fatalf("os_signature_probe_failed: %s", r.Failure)
	}
}
