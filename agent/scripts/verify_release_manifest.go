// Command verify_release_manifest checks a downloaded Streamers-Forks
// release asset (Sunshine or Punktfunk) against the Ed25519-signed manifest
// published alongside it, closing the gap plain HTTPS-to-GitHub leaves open:
// a compromised GITHUB_TOKEN/CI run could otherwise edit release assets
// after the fact and this script's own plain download would have no way to
// tell "genuine build" from "something else," same reasoning as
// itsme228/rust-shine's docs/RELEASE_SIGNING.md.
//
// Deliberately reuses USBridge-Remote's own AGENT_UPDATE_ED25519_PRIVATE_KEY
// keypair (agent/internal/update/pubkey.go's public half, duplicated below
// as publicKeyB64 -- that file is the source of truth) rather than a
// brand-new one: the agent already embeds and trusts that public key for
// its own self-update manifests, and the agent is what fetches these fork
// releases -- agent/scripts/fetch_sunshine.sh today at build time, a runtime
// launcher later once a fork is only downloaded when picked in the UI. One
// trust anchor instead of two.
//
// No dependencies beyond the Go standard library -- invoked via
// `go run agent/scripts/verify_release_manifest.go ...` straight from
// fetch_sunshine.sh, no module build step required.
//
// Usage:
//
//	go run agent/scripts/verify_release_manifest.go \
//	  -manifest manifest.json -sig manifest.json.sig \
//	  -asset Sunshine-macOS-arm64.dmg -file /path/to/downloaded/Sunshine-macOS-arm64.dmg
package main

import (
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"os"
	"strings"
)

// publicKeyB64 must stay identical to agent/internal/update/pubkey.go's
// publicKeyB64 -- that file is the source of truth; this is a deliberate
// duplicate (same reasoning as scripts/sign_update_manifest.go and
// itsme228/rust-shine's scripts/sign_usbridge_manifest.go both duplicating
// their manifest schemas independently rather than cross-importing a shared
// package) so this stays a single `go run`-able file with zero module
// resolution.
const publicKeyB64 = "k2g7jRSwmzq8DzLAxVIr9ztQ/w8P3uAkxiHy7zRlgl8="

type assetEntry struct {
	SHA256 string `json:"sha256"`
}

type manifestDoc struct {
	App     string                `json:"app"`
	Version string                `json:"version"`
	Assets  map[string]assetEntry `json:"assets"`
}

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "verify_release_manifest:", err)
		os.Exit(1)
	}
}

func run() error {
	manifestPath := flag.String("manifest", "", "path to the downloaded manifest.json")
	sigPath := flag.String("sig", "", "path to the downloaded manifest.json.sig")
	assetName := flag.String("asset", "", "asset name to check (manifest key), e.g. Sunshine-macOS-arm64.dmg")
	filePath := flag.String("file", "", "path to the downloaded file to verify")
	wantApp := flag.String("app", "streamers-forks", `expected manifest "app" field`)
	flag.Parse()

	if *manifestPath == "" || *sigPath == "" || *assetName == "" || *filePath == "" {
		return fmt.Errorf("-manifest, -sig, -asset, and -file are all required")
	}

	pub, err := base64.StdEncoding.DecodeString(publicKeyB64)
	if err != nil || len(pub) != ed25519.PublicKeySize {
		return fmt.Errorf("embedded public key is invalid")
	}

	manifestBytes, err := os.ReadFile(*manifestPath)
	if err != nil {
		return fmt.Errorf("read manifest: %w", err)
	}
	sigText, err := os.ReadFile(*sigPath)
	if err != nil {
		return fmt.Errorf("read signature: %w", err)
	}
	sig, err := base64.StdEncoding.DecodeString(strings.TrimSpace(string(sigText)))
	if err != nil {
		return fmt.Errorf("decode signature: %w", err)
	}
	if !ed25519.Verify(ed25519.PublicKey(pub), manifestBytes, sig) {
		return fmt.Errorf("manifest signature does not verify -- refusing to trust it")
	}

	var m manifestDoc
	if err := json.Unmarshal(manifestBytes, &m); err != nil {
		return fmt.Errorf("parse manifest: %w", err)
	}
	if m.App != *wantApp {
		return fmt.Errorf("manifest is for app %q, want %q", m.App, *wantApp)
	}
	entry, ok := m.Assets[*assetName]
	if !ok || len(entry.SHA256) != 64 {
		return fmt.Errorf("signed manifest %s has no entry for asset %q", m.Version, *assetName)
	}

	sum, err := sha256File(*filePath)
	if err != nil {
		return fmt.Errorf("hash %s: %w", *filePath, err)
	}
	if sum != strings.ToLower(entry.SHA256) {
		return fmt.Errorf("%s SHA-256 %s does not match signed manifest %s (%s)", *assetName, sum, m.Version, entry.SHA256)
	}

	fmt.Printf("verified %s against signed manifest %s\n", *assetName, m.Version)
	return nil
}

func sha256File(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
