package main

// Exact bytes independently observed in the owned viewer and verified against
// Windows' catalog policy in c451d77, native job380. An OS update fails closed
// until new bytes receive their own review and receipt; this is not a WinSxS or
// general signed-DLL exemption.
const verifiedGdiplusSHA = "6aa659632f947d7db52ff190293a0cdf7327918cc7da282343445ff84515cb75"
const verifiedWindowsSigner = "3b77db29ac72aa6b5880ecb2ed5ec1ec6601d847"

func verifiedGdiplusProof(name, location string, r *graphicsOSInspection) bool {
	if name != "gdiplus.dll" || location != "windows_side_by_side" || r == nil || r.Signature == nil {
		return false
	}
	s := r.Signature
	return r.FileSHA == verifiedGdiplusSHA && r.FinalPathMatches && r.Failure == "" && r.ResultFailure == "" && r.CleanupFailure == "" &&
		r.NaturalCleanup && r.CleanupJoined && !r.SafetyJobClosed && !r.TimedOut && r.SuspendedRootVerified && r.StartupHandshakeVerified &&
		r.ConsoleHostVerified && r.TotalOwnedProcesses == 2 && r.ExitCode != nil && *r.ExitCode == 0 &&
		s.Schema == 1 && s.Status == 0 && s.OSBinary && s.Type == "Catalog" && s.Publisher == "Microsoft Windows" &&
		s.Issuer == "Microsoft Windows Production PCA 2011" && s.Thumbprint == verifiedWindowsSigner && s.FileSHA == verifiedGdiplusSHA
}
