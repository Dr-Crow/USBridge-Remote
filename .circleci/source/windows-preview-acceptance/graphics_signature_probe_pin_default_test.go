//go:build windows && signatureprobe && !vm330_signature_probe

package main

const gdiplusProbeSHA = verifiedGdiplusSHA

func gdiplusProbeAssembly(string) bool { return true }
func gdiplusProbeProof(r *graphicsOSInspection) bool {
	return verifiedGdiplusProof("gdiplus.dll", "windows_side_by_side", r)
}
