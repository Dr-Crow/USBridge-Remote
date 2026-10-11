package main

import "testing"

func TestExactObservedGdiplusAdmission(t *testing.T) {
	zero := uint32(0)
	good := graphicsOSInspection{FileSHA: verifiedGdiplusSHA, FinalPathMatches: true, NaturalCleanup: true, CleanupJoined: true, SuspendedRootVerified: true, StartupHandshakeVerified: true, ConsoleHostVerified: true, TotalOwnedProcesses: 2, ExitCode: &zero, Signature: &graphicsSignature{Schema: 1, Status: 0, OSBinary: true, Type: "Catalog", Publisher: "Microsoft Windows", Issuer: "Microsoft Windows Production PCA 2011", Thumbprint: verifiedWindowsSigner, FileSHA: verifiedGdiplusSHA}}
	if !verifiedGdiplusProof("gdiplus.dll", "windows_side_by_side", &good) {
		t.Fatal("verified exact proof rejected")
	}
	for _, name := range []string{"other.dll", "gdiplus.dll.exe", "kernel32.dll", "GDIPLUS.DLL", ""} {
		if verifiedGdiplusProof(name, "windows_side_by_side", &good) {
			t.Fatal("unapproved module accepted")
		}
	}
	for _, location := range []string{"app_local", "windows_other", "other", "windows_system32"} {
		if verifiedGdiplusProof("gdiplus.dll", location, &good) {
			t.Fatal("wrong module location accepted")
		}
	}
	for _, mutate := range []func(*graphicsOSInspection){
		func(r *graphicsOSInspection) { r.FileSHA = "changed" }, func(r *graphicsOSInspection) { r.FinalPathMatches = false }, func(r *graphicsOSInspection) { r.Signature.FileSHA = "changed" },
		func(r *graphicsOSInspection) { r.Signature.OSBinary = false }, func(r *graphicsOSInspection) { r.Signature.Publisher = "Other Publisher" }, func(r *graphicsOSInspection) { r.Signature.Issuer = "Other Issuer" }, func(r *graphicsOSInspection) { r.Signature.Thumbprint = "other" },
		func(r *graphicsOSInspection) { r.Signature.Status = 1 }, func(r *graphicsOSInspection) { r.Signature.Status = 5 }, func(r *graphicsOSInspection) { r.Signature.Type = "Authenticode" },
		func(r *graphicsOSInspection) { r.Failure = "failed" }, func(r *graphicsOSInspection) { r.ResultFailure = "failed" }, func(r *graphicsOSInspection) { r.CleanupFailure = "failed" },
		func(r *graphicsOSInspection) { r.NaturalCleanup = false }, func(r *graphicsOSInspection) { r.CleanupJoined = false }, func(r *graphicsOSInspection) { r.SafetyJobClosed = true },
		func(r *graphicsOSInspection) { r.TimedOut = true },
		func(r *graphicsOSInspection) { r.ConsoleHostVerified = false }, func(r *graphicsOSInspection) { r.TotalOwnedProcesses = 3 },
		func(r *graphicsOSInspection) { r.ExitCode = nil },
		func(r *graphicsOSInspection) { nonzero := uint32(1); r.ExitCode = &nonzero }, func(r *graphicsOSInspection) { r.SuspendedRootVerified = false }, func(r *graphicsOSInspection) { r.StartupHandshakeVerified = false },
	} {
		r := good
		s := *good.Signature
		r.Signature = &s
		mutate(&r)
		if verifiedGdiplusProof("gdiplus.dll", "windows_side_by_side", &r) {
			t.Fatal("unverified proof accepted")
		}
	}
}
