//go:build signatureprobe && vm331_signature_probe

package main

// Query-only fixture for the two installed VM331 OS assemblies independently
// inventoried on 2026-10-11. This file is a test: production helper admission
// still uses verifiedGdiplusSHA and cannot enable these bytes through this tag.
const gdiplusProbeSHA = "50453fb310b9863066b40a0cda1204e86b6b86f261aa4f806e7cb89aa51215ee"

func gdiplusProbeAssembly(name string) bool {
	switch name {
	case "amd64_microsoft.windows.gdiplus_6595b64144ccf1df_1.0.26100.5074_none_7fa6f379a4bc7666",
		"amd64_microsoft.windows.gdiplus_6595b64144ccf1df_1.1.26100.5074_none_6ef8625e3dc2500d":
		return true
	}
	return false
}

// Deliberately separate query-only proof. Do not use this in module admission.
func gdiplusProbeProof(r *graphicsOSInspection) bool {
	if r == nil || r.Signature == nil {
		return false
	}
	s := r.Signature
	return r.FileSHA == gdiplusProbeSHA && r.FinalPathMatches && r.Failure == "" && r.ResultFailure == "" && r.CleanupFailure == "" &&
		r.NaturalCleanup && r.CleanupJoined && !r.SafetyJobClosed && !r.TimedOut && r.SuspendedRootVerified && r.StartupHandshakeVerified &&
		r.ConsoleHostVerified && r.TotalOwnedProcesses == 2 && r.ExitCode != nil && *r.ExitCode == 0 && !r.Accepted &&
		s.Schema == 1 && s.Status == 0 && s.OSBinary && s.Type == "Catalog" && s.Publisher == "Microsoft Windows" &&
		s.Issuer == "Microsoft Windows Production PCA 2011" && s.Thumbprint == verifiedWindowsSigner && s.FileSHA == gdiplusProbeSHA
}
