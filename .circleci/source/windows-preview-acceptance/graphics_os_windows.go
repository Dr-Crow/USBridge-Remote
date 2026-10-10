//go:build windows

package main

import (
	"os"
	"path/filepath"
	"strings"
)

type heldOSGraphics struct {
	path  string
	file  *os.File
	proof graphicsOSInspection
}

func admitObservedGdiplus(name, path, root string, pins *pins) (*graphicsOSInspection, error) {
	r := &graphicsOSInspection{}
	if name != "gdiplus.dll" || pins == nil {
		return r, failure("unapproved_os_module")
	}
	finalExpected := graphicsPath(path)
	if !strings.EqualFold(filepath.Base(finalExpected), "gdiplus.dll") || !strings.HasPrefix(strings.ToLower(finalExpected), strings.ToLower(filepath.Join(root, "WinSxS"))+string(filepath.Separator)) {
		return r, failure("os_module_location_mismatch")
	}
	if cached, ok := pins.osGraphics[name]; ok {
		// The file lock spans all three viewer cases. A different alias or copy does
		// not inherit this proof merely because its basename or contents match.
		info, e := os.Stat(path)
		held, he := cached.file.Stat()
		if !strings.EqualFold(finalExpected, cached.path) || e != nil || he != nil || !os.SameFile(info, held) {
			return r, failure("os_module_identity_changed")
		}
		proof := cached.proof
		got, e := fileSHA(cached.file, 64<<20)
		if e != nil || got != verifiedGdiplusSHA || !verifiedGdiplusProof(name, "windows_side_by_side", &proof) {
			return r, failure("os_module_cached_proof_changed")
		}
		return &proof, nil
	}
	f, err := lockFile(path)
	if err != nil {
		return r, err
	}
	accepted := false
	defer func() {
		if !accepted {
			f.Close()
		}
	}()
	final, err := finalGraphicsPath(f)
	if err != nil || !strings.EqualFold(final, finalExpected) {
		return r, failure("os_module_final_path_mismatch")
	}
	r.FinalPathMatches = true
	r.FileSHA, err = fileSHA(f, 64<<20)
	if err != nil || r.FileSHA != verifiedGdiplusSHA {
		return r, failure("os_module_hash_not_approved")
	}
	if err = runGraphicsSignature(final, root, r); err != nil {
		return r, err
	}
	if !verifiedGdiplusProof(name, "windows_side_by_side", r) {
		return r, failure("os_module_trust_not_approved")
	}
	r.Accepted = true
	if pins.osGraphics == nil {
		pins.osGraphics = map[string]heldOSGraphics{}
	}
	pins.osGraphics[name] = heldOSGraphics{path: final, file: f, proof: *r}
	pins.files = append(pins.files, f)
	accepted = true
	return r, nil
}
