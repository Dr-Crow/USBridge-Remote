//go:build signatureprobe && vm330_signature_probe

package main

import "testing"

func TestVM330QueryOnlyGdiplusProof(t *testing.T) {
	zero := uint32(0)
	good := graphicsOSInspection{FileSHA: gdiplusProbeSHA, FinalPathMatches: true, NaturalCleanup: true, CleanupJoined: true, SuspendedRootVerified: true, StartupHandshakeVerified: true, ConsoleHostVerified: true, TotalOwnedProcesses: 2, ExitCode: &zero, Signature: &graphicsSignature{Schema: 1, Status: 0, OSBinary: true, Type: "Catalog", Publisher: "Microsoft Windows", Issuer: "Microsoft Windows Production PCA 2011", Thumbprint: verifiedWindowsSigner, FileSHA: gdiplusProbeSHA}}
	if !gdiplusProbeProof(&good) {
		t.Fatal("verified exact proof rejected")
	}
	if verifiedGdiplusProof("gdiplus.dll", "windows_side_by_side", &good) {
		t.Fatal("VM query fixture must not enable production module admission")
	}
	for _, assembly := range []string{"", "amd64_microsoft.windows.gdiplus_changed", "../amd64_microsoft.windows.gdiplus_6595b64144ccf1df_1.0.26100.5074_none_7fa6f379a4bc7666"} {
		if gdiplusProbeAssembly(assembly) {
			t.Fatal("unknown assembly accepted")
		}
	}
	for _, assembly := range []string{"amd64_microsoft.windows.gdiplus_6595b64144ccf1df_1.0.26100.5074_none_7fa6f379a4bc7666", "amd64_microsoft.windows.gdiplus_6595b64144ccf1df_1.1.26100.5074_none_6ef8625e3dc2500d"} {
		if !gdiplusProbeAssembly(assembly) {
			t.Fatal("inventoried assembly rejected")
		}
	}
	for _, mutate := range []func(*graphicsOSInspection){
		func(r *graphicsOSInspection) { r.FileSHA = "changed" }, func(r *graphicsOSInspection) { r.Accepted = true }, func(r *graphicsOSInspection) {
			r.FileSHA = verifiedGdiplusSHA
			r.Signature.FileSHA = verifiedGdiplusSHA
		}, func(r *graphicsOSInspection) { r.FinalPathMatches = false }, func(r *graphicsOSInspection) { r.Signature.FileSHA = "changed" },
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
		if gdiplusProbeProof(&r) {
			t.Fatal("unverified proof accepted")
		}
	}
}
