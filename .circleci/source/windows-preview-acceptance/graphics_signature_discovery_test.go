package main

import (
	"strings"
	"testing"
)

// Passive discovery only. Neither a matching directory name nor this predicate
// permits loading/admitting a module; the native query requires fixed file bytes.
func gdiplusAssemblyCandidate(name string) bool {
	if len(name) > 256 || strings.ContainsAny(name, `/\:`) {
		return false
	}
	for _, c := range name {
		if !((c >= 'A' && c <= 'Z') || (c >= 'a' && c <= 'z') || (c >= '0' && c <= '9') || c == '_' || c == '.' || c == '-') {
			return false
		}
	}
	lower := strings.ToLower(name)
	return strings.HasPrefix(lower, "amd64_") && strings.Contains(lower, "gdiplus")
}

func TestGDIPlusDiscoveryHandlesAssemblyNamingWithoutTrust(t *testing.T) {
	for _, name := range []string{"amd64_microsoft.windows.gdiplus_test", "AMD64_Microsoft-Windows-GdiPlus_test"} {
		if !gdiplusAssemblyCandidate(name) {
			t.Fatal("bounded native assembly candidate omitted")
		}
	}
	for _, name := range []string{"x86_microsoft.windows.gdiplus_test", "amd64_other", `amd64_gdiplus\outside`, "amd64_gdiplus/../outside", "amd64_gdiplus:stream", "amd64_gdiplus\n", strings.Repeat("x", 257)} {
		if gdiplusAssemblyCandidate(name) {
			t.Fatal("invalid assembly candidate accepted")
		}
	}
}
