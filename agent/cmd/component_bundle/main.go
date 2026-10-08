// component_bundle retrieves unchanged vendor components for the authorized
// test bundle. Tokens remain in memory; only signed release metadata and
// checksum-verified archives are written. Nothing is installed or executed.
package main

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"flag"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"time"

	"usbridge_agent/internal/entitlement"
	"usbridge_agent/internal/hwid"
	"usbridge_agent/internal/streamerlaunch"
)

func main() {
	out := flag.String("out", "components", "output directory for unchanged component archives")
	target := flag.String("platform", "", "target windows/amd64, linux/amd64 or darwin/arm64; default is current platform")
	fromArchives := flag.Bool("from-archives", false, "assemble existing signed archives without network access")
	flag.Parse()
	var err error
	if *fromArchives {
		err = bundleFromArchives(*out, *target)
	} else {
		err = runForPlatform(*out, *target)
	}
	if err != nil {
		// Do not echo HTTP errors containing signed URLs or token material.
		fmt.Fprintln(os.Stderr, "component bundle:", err)
		os.Exit(1)
	}
}

type asset struct {
	Asset  string `json:"asset"`
	SHA256 string `json:"sha256"`
}
type manifest struct {
	App       string           `json:"app"`
	Version   string           `json:"version"`
	Platforms map[string]asset `json:"platforms"`
	Broker    map[string]asset `json:"broker"`
}

func verifiedAsset(info *entitlement.DownloadInfo, component, platform string) (asset, []byte, error) {
	raw, err := base64.StdEncoding.DecodeString(info.Manifest)
	if err != nil {
		return asset{}, nil, fmt.Errorf("invalid release manifest encoding")
	}
	if _, err := streamerlaunch.VerifyManifest(raw, []byte(info.ManifestSig), streamerlaunch.ReleasePublicKey()); err != nil {
		return asset{}, nil, fmt.Errorf("release manifest signature did not verify")
	}
	var m manifest
	if err := json.Unmarshal(raw, &m); err != nil {
		return asset{}, nil, err
	}
	if m.Version != info.Version {
		return asset{}, nil, fmt.Errorf("release version mismatch")
	}
	a, ok := m.Platforms[platform]
	if component == "usb-broker" {
		a, ok = m.Broker[platform]
	}
	if !ok || a.SHA256 != info.SHA256 || a.Asset == "" || filepath.Base(a.Asset) != a.Asset {
		return asset{}, nil, fmt.Errorf("signed asset metadata mismatch")
	}
	return a, raw, nil
}

func run(out string) error { return runForPlatform(out, "") }

func runForPlatform(out, target string) error {
	goos, goarch, platform, err := bundlePlatform(target)
	if err != nil {
		return err
	}
	id, err := hwid.Get()
	if err != nil {
		return fmt.Errorf("build-machine identity unavailable")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	issued, err := entitlement.RefreshLicense(ctx, id)
	if err != nil {
		return fmt.Errorf("vendor entitlement request failed")
	}
	if _, err := entitlement.VerifyForHardware(issued.Token, id); err != nil {
		return fmt.Errorf("vendor entitlement did not verify")
	}
	if err := os.MkdirAll(out, 0755); err != nil {
		return err
	}
	var sums string
	var localInputs []bundleInput
	for _, component := range []string{"rustshine", "usb-broker"} {
		var info *entitlement.DownloadInfo
		if component == "rustshine" {
			info, err = entitlement.ResolveDownload(ctx, issued.Token, platform)
		} else {
			info, err = entitlement.ResolveUSBBrokerDownload(ctx, issued.Token, platform)
		}
		if err != nil {
			return fmt.Errorf("%s download authorization failed", component)
		}
		a, raw, err := verifiedAsset(info, component, platform)
		if err != nil {
			return err
		}
		u, err := url.Parse(info.URL)
		if err != nil || u.Scheme != "https" || u.Hostname() != "release-assets.githubusercontent.com" {
			return fmt.Errorf("unexpected component download origin")
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, info.URL, nil)
		if err != nil {
			return fmt.Errorf("invalid download request")
		}
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return fmt.Errorf("%s archive transfer failed", component)
		}
		if resp.StatusCode != http.StatusOK {
			resp.Body.Close()
			return fmt.Errorf("%s archive HTTP %d", component, resp.StatusCode)
		}
		path := filepath.Join(out, a.Asset)
		f, err := os.Create(path + ".partial")
		if err != nil {
			resp.Body.Close()
			return err
		}
		h := sha256.New()
		n, copyErr := io.Copy(io.MultiWriter(f, h), io.LimitReader(resp.Body, 512<<20))
		resp.Body.Close()
		closeErr := f.Close()
		if copyErr != nil || closeErr != nil || n != info.SizeBytes || hex.EncodeToString(h.Sum(nil)) != a.SHA256 {
			os.Remove(path + ".partial")
			return fmt.Errorf("%s archive size/hash verification failed", component)
		}
		if err := os.Rename(path+".partial", path); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, component+"-manifest.json"), raw, 0644); err != nil {
			return err
		}
		if err := os.WriteFile(filepath.Join(out, component+"-manifest.sig"), []byte(info.ManifestSig), 0644); err != nil {
			return err
		}
		sums += a.SHA256 + "  " + a.Asset + "\n"
		localInputs = append(localInputs, bundleInput{Name: component, Version: info.Version, Asset: a.Asset, SHA256: a.SHA256})
		fmt.Printf("Verified %s %s (%d bytes)\n", component, info.Version, n)
	}
	if err := writeLocalBundle(out, localInputs, goos, goarch); err != nil {
		return err
	}
	return os.WriteFile(filepath.Join(out, "SHA256SUMS.txt"), []byte(sums), 0644)
}
