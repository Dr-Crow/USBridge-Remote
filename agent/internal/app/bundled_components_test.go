package app

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/localruntime"
	"usbridge_agent/internal/netpolicy"
)

func bundleMetadataFixture(t *testing.T, dir, platform string, alter func(*localcomponents.Manifest)) {
	t.Helper()
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	m := localcomponents.Manifest{Schema: 1}
	for _, name := range []string{"rustshine", "broker"} {
		kind, exe := "rustshine", "usbridge-streamer"
		if name == "broker" {
			kind, exe = "usb-broker", "usbridge-usb-broker"
		}
		windows := platform == "windows/amd64"
		if windows {
			exe += ".exe"
		}
		c := localcomponents.Component{Name: name, Platform: platform, Version: "usbridge-streamer-v0.3.131", Profile: "audited-vendor-v0.3.131", Entry: name + "/" + exe}
		files := []string{exe}
		if windows && name == "rustshine" {
			files = append(files, "libopus-0.dll")
		}
		for _, file := range files {
			hash, ok := localruntime.PinnedBundleFileHash(platform, kind, file)
			if !ok {
				t.Skip("no audited profile for test platform")
			}
			c.Files = append(c.Files, localcomponents.File{Path: name + "/" + file, Size: 1, SHA256: hash})
		}
		m.Components = append(m.Components, c)
	}
	if alter != nil {
		alter(&m)
	}
	raw, err := json.Marshal(m)
	if err != nil {
		t.Fatal(err)
	}
	sum := sha256.Sum256(raw)
	marker, _ := json.Marshal(bundledSource{Schema: 1, ManifestSHA256: hex.EncodeToString(sum[:]), DefaultBackend: "rustshine"})
	if err := os.WriteFile(filepath.Join(dir, "manifest.json"), raw, 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bundle.json"), marker, 0600); err != nil {
		t.Fatal(err)
	}
}
func TestAutomaticBundleMetadataRequiresPinnedPair(t *testing.T) {
	for _, platform := range []string{"windows/amd64", "linux/amd64", "darwin/arm64"} {
		t.Run(platform, func(t *testing.T) {
			dir := t.TempDir()
			bundleMetadataFixture(t, dir, platform, nil)
			if _, err := inspectAutomaticBundle(dir, platform); err != nil {
				t.Fatal(err)
			}
		})
	}
	for _, mutate := range []func(*localcomponents.Manifest){
		func(m *localcomponents.Manifest) {
			m.Components[0].Files[0].SHA256 = hex.EncodeToString(make([]byte, 32))
		},
		func(m *localcomponents.Manifest) { m.Components[0].Version = "unreviewed" },
		func(m *localcomponents.Manifest) { m.Components = append(m.Components, m.Components[0]) },
		func(m *localcomponents.Manifest) {
			m.Components[0].Files = append(m.Components[0].Files, localcomponents.File{Path: "rustshine/extra.dll", Size: 1, SHA256: hex.EncodeToString(make([]byte, 32))})
		},
	} {
		dir := t.TempDir()
		bundleMetadataFixture(t, dir, "windows/amd64", mutate)
		if _, err := inspectAutomaticBundle(dir, "windows/amd64"); err == nil {
			t.Fatal("unrecognized bundle accepted")
		}
	}
}
func TestAutomaticBundlePersistsNoFallbackAndLeavesUSBConsentOff(t *testing.T) {
	root := t.TempDir()
	exe := filepath.Join(root, "app")
	dir := filepath.Join(exe, "components")
	cfgPath := filepath.Join(root, "state", "config.yaml")
	bundleMetadataFixture(t, dir, runtime.GOOS+"/"+runtime.GOARCH, nil)
	cfg, err := loadStartupConfig(cfgPath, exe)
	if err != nil {
		t.Fatal(err)
	}
	if !cfg.StrictLAN || !cfg.RuntimeLocal || cfg.PreferredBackend != "rustshine" || !cfg.StreamerConsent || cfg.USBBrokerConsentGiven() {
		t.Fatal("unexpected bundled defaults")
	}
	if err := os.Rename(dir, dir+"-removed"); err != nil {
		t.Fatal(err)
	}
	again, err := loadStartupConfig(cfgPath, exe)
	if err != nil {
		t.Fatal(err)
	}
	if !again.StrictLAN || again.LocalComponentDirectory != cfg.LocalComponentDirectory || again.LocalComponentManifestSHA256 != cfg.LocalComponentManifestSHA256 {
		t.Fatal("missing bundle enabled fallback")
	}
}
func TestExplicitSourceWinsOverAutomaticBundle(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, "config.yaml")
	cfg := config.Default()
	cfg.LocalComponentDirectory = filepath.Join(root, "manual")
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	dir := filepath.Join(root, "components")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(dir, "bundle.json"), []byte("invalid"), 0600); err != nil {
		t.Fatal(err)
	}
	got, err := loadStartupConfig(cfgPath, root)
	if err != nil || got.LocalComponentDirectory != cfg.LocalComponentDirectory {
		t.Fatal("explicit source replaced", err)
	}
}

func TestAutomaticBundlePreservesExistingBackendChoice(t *testing.T) {
	root := t.TempDir()
	cfgPath := filepath.Join(root, "config.yaml")
	cfg := config.Default()
	cfg.PreferredBackend = "sunshine"
	if err := config.Save(cfgPath, cfg); err != nil {
		t.Fatal(err)
	}
	bundleMetadataFixture(t, filepath.Join(root, "components"), runtime.GOOS+"/"+runtime.GOARCH, nil)
	got, err := loadStartupConfig(cfgPath, root)
	if err != nil {
		t.Fatal(err)
	}
	if got.PreferredBackend != "sunshine" || got.StreamerConsent {
		t.Fatal("existing backend choice was replaced")
	}
}

// An explicit fixture directory contains original, signed/profile-pinned files.
// This tests offline discovery/staging, not GUI, capture or physical devices.
func TestAutomaticBundledPairOfflineFixture(t *testing.T) {
	fixture := os.Getenv("USBRIDGE_BUNDLE_FIXTURES")
	if fixture == "" {
		t.Skip("explicit audited component fixture directory required")
	}
	platform := runtime.GOOS + "/" + runtime.GOARCH
	if _, err := inspectAutomaticBundle(fixture, platform); err != nil {
		t.Fatal(err)
	}
	raw, err := os.ReadFile(filepath.Join(fixture, "manifest.json"))
	if err != nil {
		t.Fatal(err)
	}
	var m localcomponents.Manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		t.Fatal(err)
	}
	root := t.TempDir()
	exe := filepath.Join(root, "agent")
	dir := filepath.Join(exe, "components")
	if err := os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	paths := []string{"manifest.json", "bundle.json"}
	for _, c := range m.Components {
		for _, f := range c.Files {
			paths = append(paths, f.Path)
		}
	}
	for _, name := range paths {
		data, err := os.ReadFile(filepath.Join(fixture, filepath.FromSlash(name)))
		if err != nil {
			t.Fatal(err)
		}
		dest := filepath.Join(dir, filepath.FromSlash(name))
		if err := os.MkdirAll(filepath.Dir(dest), 0700); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(dest, data, 0700); err != nil {
			t.Fatal(err)
		}
	}
	cfg, err := loadStartupConfig(filepath.Join(root, "config.yaml"), exe)
	if err != nil {
		t.Fatal(err)
	}
	cfg.StateDir = filepath.Join(root, "state")
	t.Setenv(netpolicy.Environment, "1")
	netpolicy.ConfigureRuntimeLocal(true)
	defer netpolicy.ConfigureRuntimeLocal(false)
	t.Setenv(localruntime.Environment, "1")
	defer localruntime.Close()
	for _, name := range []string{"rustshine", "broker"} {
		result, err := localcomponents.Resolve(context.Background(), localOptions(cfg), name)
		if err != nil {
			t.Fatal(err)
		}
		kind := name
		if kind == "broker" {
			kind = "usb-broker"
		}
		original, err := os.ReadFile(result.Binary)
		if err != nil {
			t.Fatal(err)
		}
		before := sha256.Sum256(original)
		if _, err := localruntime.Prepare(result.Binary, cfg.StateDir, kind, "offline-fixture-test"); err != nil {
			t.Fatal(err)
		}
		after, err := os.ReadFile(result.Binary)
		if err != nil || sha256.Sum256(after) != before {
			t.Fatal("staged original changed")
		}
	}
	if cfg.EntitlementToken != "" || cfg.USBBrokerConsentGiven() {
		t.Fatal("offline fixture fabricated vendor credentials or sharing consent")
	}
	t.Log("Automatic bundle discovered; both audited originals staged and copies prepared with strict public-network guards; no GUI, stream or device acceptance claimed")
}
