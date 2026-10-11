//go:build windows && signatureprobe

package main

import (
	"encoding/json"
	"io"
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
	for _, fixture := range []struct{ name, script, want string }{
		{"missing_marker", "$null=[Console]::In.ReadLine()", "signature_startup_timeout"},
		{"early_output", "[Console]::Out.WriteLine('{}');$null=[Console]::In.ReadLine()", "signature_startup_marker_invalid"},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			observed := graphicsOSInspection{}
			err := runGraphicsSignatureScript("", root, &observed, fixture.script)
			if err == nil || err.Error() != fixture.want || !observed.CleanupJoined || observed.NaturalCleanup || !observed.SafetyJobClosed || observed.StartupHandshakeVerified || observed.Signature != nil || observed.Accepted {
				t.Fatal("signature_negative_lifecycle_failed")
			}
		})
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
	passed := !t.Failed() && r.Failure == "" && r.CleanupJoined && r.SuspendedRootVerified && r.StartupHandshakeVerified && !r.SafetyJobClosed && r.ConsoleHostVerified && r.TotalOwnedProcesses == 2 && r.NaturalCleanup && r.Signature != nil && r.Signature.Status == 0 && r.Signature.OSBinary
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

// Query only the installed GDI+ bytes already observed in the owned viewer.
// This fast prerequisite does not load the DLL or admit a viewer module. The
// later viewer gate still verifies its actual loaded path and retained lock.
func TestWindowsSignatureVerifierExactGDIPlus(t *testing.T) {
	out, root := os.Getenv("WINDOWS_SIGNATURE_GDIPLUS_RECEIPT"), os.Getenv("SystemRoot")
	if !drivePath.MatchString(out) || !drivePath.MatchString(root) {
		t.Fatal("gdiplus_probe_configuration_missing")
	}
	if _, err := os.Lstat(out); !os.IsNotExist(err) {
		t.Fatal("gdiplus_receipt_must_be_new")
	}
	proofs := []graphicsOSInspection{}
	entriesScanned, assemblyCandidates := 0, 0
	assemblyNames := []string{}
	probe := func() (result error) {
		// Discovery is read-only and does not confer trust. WinSxS assembly
		// names vary by OS image; selection still requires the exact known
		// file bytes plus the unchanged final-path/catalog/publisher proof.
		directory, err := os.Open(filepath.Join(root, "WinSxS"))
		if err != nil {
			return failure("gdiplus_directory_open_failed")
		}
		paths := []string{}
		for {
			entries, readErr := directory.ReadDir(256)
			entriesScanned += len(entries)
			if entriesScanned > 65536 {
				directory.Close()
				return failure("gdiplus_directory_count_bound")
			}
			for _, entry := range entries {
				if !entry.IsDir() || !gdiplusAssemblyCandidate(entry.Name()) {
					continue
				}
				assemblyCandidates++
				if assemblyCandidates > 128 {
					directory.Close()
					return failure("gdiplus_candidate_count_bound")
				}
				assemblyNames = append(assemblyNames, entry.Name())
				paths = append(paths, filepath.Join(root, "WinSxS", entry.Name(), "gdiplus.dll"))
			}
			if readErr == io.EOF {
				break
			}
			if readErr != nil {
				directory.Close()
				return failure("gdiplus_directory_read_failed")
			}
		}
		if err := directory.Close(); err != nil {
			return failure("gdiplus_directory_close_failed")
		}
		if len(paths) == 0 {
			return failure("gdiplus_no_assembly_candidates")
		}
		var chosen string
		var bytesInspected int64
		var held *os.File
		for _, path := range paths {
			info, err := os.Lstat(path)
			if os.IsNotExist(err) {
				continue
			}
			if err != nil || !info.Mode().IsRegular() {
				return failure("gdiplus_candidate_not_regular")
			}
			if info.Size() <= 0 || info.Size() > 64<<20 {
				return failure("gdiplus_candidate_size_bound")
			}
			bytesInspected += info.Size()
			if bytesInspected > 512<<20 {
				return failure("gdiplus_aggregate_size_bound")
			}
			f, err := lockFile(path)
			if err != nil {
				return failure("gdiplus_candidate_lock_failed")
			}
			got, hashErr := fileSHA(f, 64<<20)
			final, finalErr := finalGraphicsPath(f)
			if hashErr == nil && finalErr == nil && got == verifiedGdiplusSHA && strings.EqualFold(final, graphicsPath(path)) {
				chosen, held = final, f
				break
			}
			if err := f.Close(); err != nil {
				return failure("gdiplus_candidate_close_failed")
			}
		}
		if held == nil {
			return failure("gdiplus_exact_bytes_not_found")
		}
		defer func() {
			if closeErr := held.Close(); closeErr != nil && result == nil {
				result = failure("gdiplus_hold_close_failed")
			}
		}()
		for i := 0; i < 3; i++ {
			r := graphicsOSInspection{FileSHA: verifiedGdiplusSHA, FinalPathMatches: true}
			if err := runGraphicsSignature(chosen, root, &r); err != nil {
				r.Failure = err.Error()
			}
			proofs = append(proofs, r)
			if !verifiedGdiplusProof("gdiplus.dll", "windows_side_by_side", &r) || r.Accepted {
				return failure("gdiplus_exact_query_failed")
			}
		}
		return nil
	}
	err := probe()
	code := ""
	if err != nil {
		code = err.Error()
	}
	receipt := struct {
		Schema                  int                    `json:"schema_version"`
		Commit                  string                 `json:"commit"`
		Passed                  bool                   `json:"passed"`
		Failure                 string                 `json:"failure_code,omitempty"`
		ExpectedSHA             string                 `json:"expected_file_sha256"`
		Inspections             []graphicsOSInspection `json:"inspections"`
		ModuleAccepted          bool                   `json:"viewer_module_accepted"`
		DirectoryEntriesScanned int                    `json:"directory_entries_scanned"`
		AssemblyCandidates      int                    `json:"assembly_candidates"`
		AssemblyNames           []string               `json:"assembly_names"`
	}{1, os.Getenv("CIRCLE_SHA1"), err == nil && len(proofs) == 3, code, verifiedGdiplusSHA, proofs, false, entriesScanned, assemblyCandidates, assemblyNames}
	if !commitPattern.MatchString(receipt.Commit) {
		t.Fatal("gdiplus_probe_commit_missing")
	}
	raw, marshalErr := json.MarshalIndent(receipt, "", "  ")
	if marshalErr != nil || writeReceipt(out, append(raw, '\n')) != nil {
		t.Fatal("gdiplus_receipt_failed")
	}
	if !receipt.Passed {
		t.Fatalf("gdiplus_probe_failed: %s", code)
	}
}
