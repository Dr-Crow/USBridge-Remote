package localruntime

// Exact extracted executable hashes from the verified v0.3.131 archives.
var pinned = map[string]string{
	"linux/amd64/rustshine":    "b4b10300d17cccba29563f489238bbb2eb1fcc79d8aa43f1506df89c37a10a19",
	"darwin/arm64/rustshine":   "313f9fd3bfc9c770087dbce4b07776f46cde3a3fbbe6612dd4bfd362d4e744aa",
	"windows/amd64/rustshine":  "4b36e53d23861c1e85983dc1843ab31374337fd608af56db113714188e69a542",
	"linux/amd64/usb-broker":   "61ec85208aa0ed0daafb5f20727ceacfcbfe76a2ff48c06cbb146859004fa399",
	"darwin/arm64/usb-broker":  "ca37f6157198a079940a96cfe20251183539488ce0a4e5d3e04730f33107c88d",
	"windows/amd64/usb-broker": "525c054ea9b7422a56423548fb25707901e8822994ad938772f484f0acf2e3d1",
}

const pinnedWindowsCodecSHA256 = "c12f9a671a106c649a5517d28061670aa57c805d242b80764853a7551099498d"

// PinnedBundleFileHash identifies only the audited executable and its known
// dependency. Automatic bundles may not introduce arbitrary adjacent programs.
func PinnedBundleFileHash(platform, component, filename string) (string, bool) {
	hash, ok := pinned[platform+"/"+component]
	if !ok {
		return "", false
	}
	suffix := ""
	if len(platform) >= 8 && platform[:8] == "windows/" {
		suffix = ".exe"
	}
	base := "usbridge-streamer"
	if component == "usb-broker" {
		base = "usbridge-usb-broker"
	}
	if filename == base+suffix {
		return hash, true
	}
	if component == "rustshine" && suffix == ".exe" && filename == "libopus-0.dll" {
		return pinnedWindowsCodecSHA256, true
	}
	return "", false
}
