# Automatic prebundled setup

The Windows combined ZIP places `components/` beside `USBridgeAgent.exe` inside
its `agent/` directory. Run that EXE normally. No source-path setting, runtime flag
or license token is needed for its included audited streamer pair.

On startup the agent recognizes `components/bundle.json`, verifies its manifest
hash, and accepts only the exact reviewed v0.3.131 executable/dependency hashes
for Windows x86_64, Linux x86_64 or macOS ARM64. It does not automatically trust
an arbitrary neighboring manifest or an unknown updated binary. macOS apps also
look inside `Contents/Resources/components`; support for a matching component
pair does not imply a Mac Intel RustShine build exists.

The selected directory and manifest pin are saved with strict offline provisioning
and local runtime networking. If the original bundle later disappears, the agent
uses its verified installed copy or reports an error; it never switches to public
downloads. Sources explicitly configured by the operator take precedence.

A fresh/unselected install of the marked bundled edition selects its included
RustShine backend. Existing explicit backend choices are retained. The streamer
files are staged and private runtime copies prepared by the existing startup
path. Original files are not modified. USB driver/device-sharing consent remains
separate; including a broker executable does not enable device sharing by itself.

The plain agent ZIP remains the ordinary selected-component download edition.
Both editions retain normal OS capture/device permissions, pairing and TLS.
The Windows service and privileged Linux KMS restrictions remain unresolved.
Unknown future versions fail closed until their profiles are reviewed.

## Produce bundles or a LAN mirror directory

The `component_bundle` build tool now emits unchanged archives, their vendor
signed manifests, expanded pinned runtime files, a schema-1 local `manifest.json`
and an automatic-discovery `bundle.json`. The result can also be supplied as an
explicit local directory or to the LAN host's component mirror.

Build this tool in a prepared build environment. Its compiled executable accepts:

- `-out PATH`: output/input component directory.
- `-platform windows/amd64`, `linux/amd64` or `darwin/arm64`: explicit target;
  omitted means the current platform.
- `-from-archives`: verify existing signed manifests and archives and assemble
  the local directory without network access. The normal mode obtains genuine
  download authorization. Tokens are never copied into the output.

The offline flag describes the compiled tool. `go run` itself can download missing
build dependencies, so do not use that as proof of an offline build environment.

## Verified so far

Linux original fixtures passed offline bundle discovery, strict staging and
private-copy preparation in a race-enabled test, without a vendor token or USB
sharing consent. Windows archives were assembled offline using their signed
metadata and exact file hashes. The new Windows CI step is configured to run the discovery/staging fixture
against its generated directory before packaging the combined ZIP; its first
result is still pending. Linux/macOS automatic discovery paths are implemented,
but their prebundled application archives are not yet added to the Unix build job.

This is not physical capture/streaming/USB or full blocked-WAN GUI acceptance.
The browser tests, local TLS trust setup and real device tests remain separate.
