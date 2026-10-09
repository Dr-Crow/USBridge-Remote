# Agent platform builds

The upstream packaging scripts support Windows x86_64, Linux x86_64 and macOS.
macOS source/packaging includes both Apple Silicon and Intel paths. This workflow
builds Windows amd64, Linux amd64, macOS arm64 and macOS amd64. There is no upstream
Windows ARM64 or Linux ARM64 agent packaging target; those are future ports rather
than existing supported builds. The vendor component manifest separately lists
Windows x86_64, Linux x86_64 and macOS ARM64 only.

Windows: extract the runtime ZIP, keeping DLLs beside the EXE. The Windows test
exclusions remain documented in WINDOWS_TEST_LIMITATIONS.md.
Linux: prefer the AppImage, which carries GUI runtime libraries. The tar.gz contains
the native binary and privileged-launcher helper and requires system GUI libraries.
macOS: ZIP contains an ad-hoc signed .app, DMG provides the same bundle. These builds
are not Apple-notarized. Intel is cross-compiled on Apple Silicon and architecture
checked; the test suite runs natively on Apple Silicon, not on Intel hardware.
Do not equate cross-compilation with an Intel runtime acceptance test.

Both Mac bundles include an architecture-matched Tailscale CLI built from the
pinned module. The system Tailscale service still needs separate setup. Sunshine
is not bundled in these test artifacts. Apple Silicon can use the existing download
path. Intel requires a compatible separately installed/configured Sunshine backend;
the vendor does not provide an Intel RustShine/broker pair in the inspected manifest.

All builds use manual agent self-update to avoid being replaced by upstream builds.
No tokens, user configuration or private keys are bundled. Quit the existing agent
and back up configuration before testing. See FORK_TEST_PLAN.md for behavioral tests.

Build inputs: Go 1.26.6 archives are SHA256 verified. Linux uses CircleCI Ubuntu 22.04
current; macOS uses Xcode 16.4.0 on M4 Pro. Platform toolchains are recorded in artifacts.

## Native packaged-agent startup probe

The Windows, Linux x86_64 and native macOS ARM64 CI jobs run the packaged agent
headlessly in a disposable profile. Each probe loads a disposable operator TLS
pair and verifies the HTTPS health endpoint using normal chain and IP-SAN
validation. No OS trust-store changes are made. The report records the exact
executable SHA256. The macOS Intel cross-build is not a native execution test.

The profile uses strict local mode, an explicitly selected unprepared Sunshine
backend, and no streamer/USB consent, so this test does not initiate capture,
install a driver, or request vendor provisioning. Component preparation has a
separate fixture test. No GUI, real media, USB device, or complete egress-isolation
acceptance is claimed by the startup probe.

On 2026-10-09 UTC the Linux agent from commit 392bf2d was also exercised by this
probe on the root Linux executor: startup and the verified HTTPS handshake passed.
