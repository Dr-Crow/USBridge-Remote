package main

import (
	"testing"
	"usbridge_agent/internal/entitlement"
)

func TestVerifiedAssetRejectsUnsignedMetadata(t *testing.T) {
	for _, value := range []string{"", "e30="} {
		if _, _, err := verifiedAsset(&entitlement.DownloadInfo{Manifest: value}, "rustshine", "windows-x86_64"); err == nil {
			t.Fatal("unsigned component metadata accepted")
		}
	}
}
