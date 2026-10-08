package app

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"
	"usbridge_agent/internal/config"
	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/localruntime"
)

type bundledSource struct {
	Schema         int    `json:"schema"`
	ManifestSHA256 string `json:"manifest_sha256"`
	DefaultBackend string `json:"default_backend"`
}

func boundedBundleFile(filename string, max int64) ([]byte, error) {
	info, err := os.Lstat(filename)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() > max {
		return nil, fmt.Errorf("invalid bundle metadata file")
	}
	f, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, max+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) > max {
		return nil, fmt.Errorf("bundle metadata exceeds limit")
	}
	return data, nil
}

// inspectAutomaticBundle trusts only the audited pair, not an arbitrary local
// manifest. File contents are subsequently rehashed by Resolve before use.
func inspectAutomaticBundle(dir, platform string) (bundledSource, error) {
	var marker bundledSource
	info, err := os.Lstat(dir)
	if err != nil {
		return marker, err
	}
	if !info.IsDir() || info.Mode()&os.ModeSymlink != 0 {
		return marker, fmt.Errorf("automatic components must be a real directory")
	}
	raw, err := boundedBundleFile(filepath.Join(dir, "bundle.json"), 4096)
	if err != nil {
		return marker, err
	}
	dec := json.NewDecoder(bytes.NewReader(raw))
	dec.DisallowUnknownFields()
	if err := dec.Decode(&marker); err != nil {
		return marker, err
	}
	if err := dec.Decode(new(any)); err != io.EOF {
		return marker, fmt.Errorf("bundle marker has trailing data")
	}
	if marker.Schema != 1 || marker.DefaultBackend != "rustshine" {
		return marker, fmt.Errorf("unsupported automatic bundle")
	}
	manifestRaw, err := boundedBundleFile(filepath.Join(dir, "manifest.json"), 1<<20)
	if err != nil {
		return marker, err
	}
	sum := sha256.Sum256(manifestRaw)
	if !strings.EqualFold(hex.EncodeToString(sum[:]), marker.ManifestSHA256) {
		return marker, fmt.Errorf("automatic bundle manifest checksum mismatch")
	}
	var manifest localcomponents.Manifest
	if err := json.Unmarshal(manifestRaw, &manifest); err != nil {
		return marker, err
	}
	if len(manifest.Components) != 2 {
		return marker, fmt.Errorf("automatic bundle must contain exactly the audited pair")
	}
	for _, name := range []string{"rustshine", "broker"} {
		c, err := localcomponents.InspectManifest(manifestRaw, name, platform, marker.ManifestSHA256)
		if err != nil {
			return marker, err
		}
		if c.Version != "usbridge-streamer-v0.3.131" || c.Profile != "audited-vendor-v0.3.131" {
			return marker, fmt.Errorf("automatic bundle profile is not audited")
		}
		kind, entry := "rustshine", "usbridge-streamer"
		if name == "broker" {
			kind, entry = "usb-broker", "usbridge-usb-broker"
		}
		windows := strings.HasPrefix(platform, "windows/")
		if windows {
			entry += ".exe"
		}
		if path.Base(c.Entry) != entry {
			return marker, fmt.Errorf("unexpected automatic bundle entry")
		}
		expectedCount := 1
		if windows && name == "rustshine" {
			expectedCount = 2
		}
		if len(c.Files) != expectedCount {
			return marker, fmt.Errorf("unexpected automatic bundle dependencies")
		}
		seen := map[string]bool{}
		for _, f := range c.Files {
			base := path.Base(f.Path)
			hash, ok := localruntime.PinnedBundleFileHash(platform, kind, base)
			if !ok || !strings.EqualFold(hash, f.SHA256) || seen[base] || !strings.HasPrefix(f.Path, name+"/") {
				return marker, fmt.Errorf("unrecognized automatic bundle file")
			}
			seen[base] = true
		}
	}
	return marker, nil
}

// loadStartupConfig discovers only an explicitly marked adjacent bundle. It
// persists strict source selection, preventing later disappearance from enabling
// public fallback. Explicit operator sources and existing backend choices win.
func loadStartupConfig(cfgPath, exeDir string) (config.Config, error) {
	cfg, err := config.Load(cfgPath)
	if err != nil {
		return cfg, err
	}
	if cfg.StrictLAN || cfg.LocalComponentDirectory != "" || cfg.LocalComponentBundle != "" || cfg.LocalComponentMirror != "" || cfg.LocalComponentManifestSHA256 != "" {
		return cfg, nil
	}
	_, statErr := os.Stat(cfgPath)
	fresh := os.IsNotExist(statErr)
	candidates := []string{filepath.Join(exeDir, "components")}
	if runtime.GOOS == "darwin" && filepath.Base(exeDir) == "MacOS" && filepath.Base(filepath.Dir(exeDir)) == "Contents" {
		candidates = append(candidates, filepath.Join(filepath.Dir(exeDir), "Resources", "components"))
	}
	for _, dir := range candidates {
		if _, err := os.Lstat(filepath.Join(dir, "bundle.json")); os.IsNotExist(err) {
			continue
		} else if err != nil {
			return cfg, err
		}
		marker, err := inspectAutomaticBundle(dir, runtime.GOOS+"/"+runtime.GOARCH)
		if err != nil {
			return cfg, fmt.Errorf("bundled components: %w", err)
		}
		dir, err = filepath.Abs(dir)
		if err != nil {
			return cfg, err
		}
		cfg.StrictLAN = true
		cfg.RuntimeLocal = true
		cfg.LocalComponentDirectory = dir
		cfg.LocalComponentManifestSHA256 = marker.ManifestSHA256
		// Downloading the marked bundled edition chooses its included streamer;
		// USB drivers/device sharing still require their normal independent consent.
		if fresh || cfg.PreferredBackend == "" {
			cfg.PreferredBackend = marker.DefaultBackend
			cfg.StreamerConsent = true
		}
		if err := config.Save(cfgPath, cfg); err != nil {
			return cfg, fmt.Errorf("save bundled component source: %w", err)
		}
		return cfg, nil
	}
	return cfg, nil
}
