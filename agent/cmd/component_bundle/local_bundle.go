package main

import (
	"archive/tar"
	"archive/zip"
	"compress/gzip"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"usbridge_agent/internal/localcomponents"
	"usbridge_agent/internal/localruntime"
	"usbridge_agent/internal/streamerlaunch"
)

type bundleInput struct{ Name, Version, Asset, SHA256 string }

// writeLocalBundle expands only the expected files from checksum-verified vendor
// archives. Originals and vendor signatures remain beside the local manifest.
func writeLocalBundle(out string, inputs []bundleInput, goos, goarch string) error {
	manifest := localcomponents.Manifest{Schema: 1}
	for _, input := range inputs {
		if input.Version != "usbridge-streamer-v0.3.131" {
			return fmt.Errorf("no audited bundle profile for release %s", input.Version)
		}
		archive := filepath.Join(out, input.Asset)
		bytes, err := os.ReadFile(archive)
		if err != nil {
			return err
		}
		sum := sha256.Sum256(bytes)
		if hex.EncodeToString(sum[:]) != input.SHA256 {
			return fmt.Errorf("bundle archive changed after verification")
		}
		name, base := "rustshine", "usbridge-streamer"
		if input.Name == "usb-broker" {
			name, base = "broker", "usbridge-usb-broker"
		} else if input.Name != "rustshine" {
			return fmt.Errorf("unsupported bundle component")
		}
		if goos == "windows" {
			base += ".exe"
		}
		wanted := map[string]bool{base: true}
		if goos == "windows" && name == "rustshine" {
			wanted["libopus-0.dll"] = true
		}
		seen := map[string]bool{}
		component := localcomponents.Component{Name: name, Platform: goos + "/" + goarch, Version: input.Version, Profile: "audited-vendor-v0.3.131", Entry: name + "/" + base}
		accept := func(raw string, size int64, r io.Reader) error {
			raw = strings.ReplaceAll(raw, "\\", "/")
			if path.IsAbs(raw) || path.Clean(raw) != raw || strings.Contains(raw, ":") {
				return fmt.Errorf("unsafe archive member")
			}
			for _, part := range strings.Split(raw, "/") {
				if part == ".." {
					return fmt.Errorf("unsafe archive traversal")
				}
			}
			filename := path.Base(raw)
			if !wanted[filename] {
				return nil
			}
			if seen[filename] || size <= 0 || size > 512<<20 {
				return fmt.Errorf("duplicate or invalid bundle file")
			}
			data, err := io.ReadAll(io.LimitReader(r, size+1))
			if err != nil {
				return err
			}
			if int64(len(data)) != size {
				return fmt.Errorf("bundle member size mismatch")
			}
			digest := sha256.Sum256(data)
			fileHash := hex.EncodeToString(digest[:])
			approved, known := localruntime.PinnedBundleFileHash(goos+"/"+goarch, input.Name, filename)
			if !known || fileHash != approved {
				return fmt.Errorf("bundle file is outside the audited profile")
			}
			seen[filename] = true
			rel := name + "/" + filename
			dest := filepath.Join(out, filepath.FromSlash(rel))
			if err := os.MkdirAll(filepath.Dir(dest), 0755); err != nil {
				return err
			}
			mode := os.FileMode(0644)
			if filename == base {
				mode = 0700
			}
			if err := os.WriteFile(dest, data, mode); err != nil {
				return err
			}
			component.Files = append(component.Files, localcomponents.File{Path: rel, Size: size, SHA256: fileHash, Executable: filename == base})
			return nil
		}
		if strings.HasSuffix(input.Asset, ".zip") {
			z, err := zip.OpenReader(archive)
			if err != nil {
				return err
			}
			for _, f := range z.File {
				if f.FileInfo().IsDir() {
					continue
				}
				if !f.Mode().IsRegular() {
					z.Close()
					return fmt.Errorf("nonregular ZIP member")
				}
				reader, err := f.Open()
				if err != nil {
					z.Close()
					return err
				}
				err = accept(f.Name, int64(f.UncompressedSize64), reader)
				reader.Close()
				if err != nil {
					z.Close()
					return err
				}
			}
			z.Close()
		} else {
			f, err := os.Open(archive)
			if err != nil {
				return err
			}
			gz, err := gzip.NewReader(f)
			if err != nil {
				f.Close()
				return err
			}
			tr := tar.NewReader(gz)
			for {
				h, err := tr.Next()
				if err == io.EOF {
					break
				}
				if err != nil {
					gz.Close()
					f.Close()
					return err
				}
				if h.Typeflag == tar.TypeDir {
					continue
				}
				if h.Typeflag != tar.TypeReg && h.Typeflag != tar.TypeRegA {
					gz.Close()
					f.Close()
					return fmt.Errorf("nonregular TAR member")
				}
				if err := accept(h.Name, h.Size, tr); err != nil {
					gz.Close()
					f.Close()
					return err
				}
			}
			gz.Close()
			f.Close()
		}
		if len(seen) != len(wanted) {
			return fmt.Errorf("missing required bundle files for %s", name)
		}
		sort.Slice(component.Files, func(i, j int) bool { return component.Files[i].Path < component.Files[j].Path })
		manifest.Components = append(manifest.Components, component)
	}
	if len(manifest.Components) != 2 {
		return fmt.Errorf("complete streamer and broker pair required")
	}
	raw, err := json.MarshalIndent(manifest, "", "  ")
	if err != nil {
		return err
	}
	raw = append(raw, '\n')
	if err := os.WriteFile(filepath.Join(out, "manifest.json"), raw, 0644); err != nil {
		return err
	}
	sum := sha256.Sum256(raw)
	marker := struct {
		Schema         int    `json:"schema"`
		ManifestSHA256 string `json:"manifest_sha256"`
		DefaultBackend string `json:"default_backend"`
	}{1, hex.EncodeToString(sum[:]), "rustshine"}
	markerBytes, err := json.MarshalIndent(marker, "", "  ")
	if err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "bundle.json"), append(markerBytes, '\n'), 0644)
}
func bundlePlatform(target string) (string, string, string, error) {
	if target == "" {
		target = runtime.GOOS + "/" + runtime.GOARCH
	}
	switch target {
	case "windows/amd64":
		return "windows", "amd64", "windows-x86_64", nil
	case "linux/amd64":
		return "linux", "amd64", "linux-x86_64", nil
	case "darwin/arm64":
		return "darwin", "arm64", "macos-arm64", nil
	default:
		return "", "", "", fmt.Errorf("no audited component pair for %s", target)
	}
}

// bundleFromArchives is a fully offline, signature-checked repackaging path.
func bundleFromArchives(out, target string) error {
	goos, goarch, platform, err := bundlePlatform(target)
	if err != nil {
		return err
	}
	var inputs []bundleInput
	for _, name := range []string{"rustshine", "usb-broker"} {
		raw, err := os.ReadFile(filepath.Join(out, name+"-manifest.json"))
		if err != nil {
			return err
		}
		sig, err := os.ReadFile(filepath.Join(out, name+"-manifest.sig"))
		if err != nil {
			return err
		}
		if _, err := streamerlaunch.VerifyManifest(raw, sig, streamerlaunch.ReleasePublicKey()); err != nil {
			return fmt.Errorf("offline manifest signature invalid")
		}
		var m manifest
		if err := json.Unmarshal(raw, &m); err != nil {
			return err
		}
		a, ok := m.Platforms[platform]
		if name == "usb-broker" {
			a, ok = m.Broker[platform]
		}
		if !ok || a.Asset == "" || filepath.Base(a.Asset) != a.Asset {
			return fmt.Errorf("offline platform asset missing or invalid")
		}
		inputs = append(inputs, bundleInput{Name: name, Version: m.Version, Asset: a.Asset, SHA256: a.SHA256})
	}
	return writeLocalBundle(out, inputs, goos, goarch)
}
