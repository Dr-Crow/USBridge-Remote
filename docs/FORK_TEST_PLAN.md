## Current normal-launch test builds

Supported interactive builds now enable the local runtime by default. Quit any old
engine, extract the complete runtime ZIP to a new directory and run the agent
normally. No initial local-runtime flag or toggle is needed. Explicitly disabled
advanced overrides still apply. The marked combined bundle additionally discovers
its adjacent components automatically; see [AUTOMATIC_BUNDLES.md](AUTOMATIC_BUNDLES.md).
Earlier flag-based instructions below describe the original research build.

# First Windows capability-policy test build

## Scope

This branch builds the **public Go agent**, not the proprietary RustShine streamer
or USB broker. It does not modify their binaries, verification keys or device
policies. It keeps genuine vendor entitlement, pairing, TLS and download integrity.

Implemented in this iteration:

- Sunshine 4:4:4 availability follows the existing encoder probe, regardless of
  the agent's subscription tier. Decoder/protocol support is still required.
- The cosmetic Pro theme can be selected, persisted and restored without a paid
  account. Selecting a theme does not change the actual entitlement.
- One **USBridge streamer** backend choice replaces the Free/Pro backend tiles.
  Its information dialog explains real capabilities rather than offering checkout.
- Selecting that backend opts into provisioning once. The agent requests a genuine
  vendor token if needed, verifies it, downloads the component, and retries an
  interrupted/offline setup in the background. Sunshine remains the initial
  open-source alternative; simply launching a fresh install does not consent to
  running proprietary code.
- USB retains its separate explicit enable/driver/device-sharing permissions.
  Enabling it obtains an entitlement if necessary and stages the broker. No device
  is automatically shared. Broker refusal details remain visible.
- Startup entitlement refresh now uses the same retry-owning loop as later
  refreshes: six hours normally, five minutes after failure. Signed expiration is
  still enforced; this is not permanent offline authorization.

## CircleCI build

`.circleci/config.yml` replaces the onboarding Hello World job. Windows Server
2022 image `2026.05.1`, Go 1.26.6 and the MSYS2 2026-09-27 bootstrap are pinned.
Go/MSYS2 archive checksums are checked before use. MSYS2 package repositories are
rolling; the actual installed versions are captured in `build-info.txt`. This is
an auditable build, not a claim of bit-for-bit reproducibility across future runs.

The job runs module verification, Go tests with Fyne's software `ci` driver,
Windows vet (the upstream Win32 uintptr false-positive analyzer exclusion is
retained), and the existing native `agent/scripts/build_windows.sh`. The shipped
EXE is built **without** the test-only `ci` tag.

Artifacts: `USBridgeAgent.exe`, its runtime-DLL ZIP, `SHA256SUMS.txt`, source commit,
toolchain/package inventory, test/vet/build logs and this checklist. No account
tokens, machine IDs or private keys belong in artifacts. The explicitly requested
combined ZIP includes unchanged Windows vendor components. A small CI utility
requests a genuine free entitlement for the disposable build machine, keeps that
token only in memory, verifies each vendor release-manifest signature, and checks
archive size/SHA256 before packaging. It never runs the vendor binaries. The old
GitHub Actions YAML is inactive in `.github/workflows-reference/`. No release,
deployment, signing credential or upstream workflow is triggered by this config.
The agent CI build uses a manual self-update channel so an upstream update cannot
replace the fork being tested; signature verification code remains intact.

CircleCI artifacts are temporary outputs, not a permanent component CDN. Official
docs currently state 30-day default and maximum retention; private-project
artifacts are account protected. This repository is public: do not assume its
artifacts are private. Storage/access and organization limits must be checked
before putting restricted vendor files there.

References: [Windows executor](https://circleci.com/docs/guides/execution-managed/using-windows/),
[artifacts](https://circleci.com/docs/guides/optimize/artifacts/),
[MSYS2 CI](https://www.msys2.org/docs/ci/).

## Windows acceptance checklist

1. Download the ZIP from the final commit's CircleCI **Artifacts** tab. Verify its
   SHA256 against `SHA256SUMS.txt`, extract the entire ZIP, and keep runtime DLLs
   beside the EXE. The separate EXE is useful for identification but is not promised
   to run alone on a clean Windows installation. This test build is unsigned;
   do not bypass security warnings or install it as a system service for the test.
2. Back up your existing agent configuration and quit the old agent completely.
   Do not test two copies against the same configuration simultaneously.
3. Confirm one USBridge streamer tile, Sunshine alternative, and no Free/Pro
   streamer purchase choice. Open information, close it, select repeatedly, cancel
   the initial proprietary-component confirmation, and verify cancellation did not
   enable or download the component.
4. Select the Pro cosmetic theme on a free/unlinked setup; restart and verify the
   choice persists. Default theme must still follow actual backend status.
5. With Sunshine and compatible encoder/client, request 4:4:4. Confirm the
   negotiated format in client/backend stats. Unsupported hardware must remain
   unavailable; an enabled UI alone is not a successful stream test.
6. Select USBridge streamer, confirm its component prompt, and verify token setup
   and download complete without choosing a tariff. Repeat from a temporarily
   offline start and reconnect; setup should retry without a six-hour startup gap.
   A missing/expired token cannot be renewed offline.
7. USB remains disabled until its own consent. Enable it, check broker status,
   grant/install only the driver permission you intend, and explicitly select a
   test device. Try detach/reconnect and verify real errors are shown. Paid device
   classes may still be refused by the unchanged broker.
8. Return to Sunshine and restart: a prior pending streamer setup must not switch
   you back unexpectedly. Confirm pairing and authentication still apply.

Record the commit, artifact hash, host GPU/driver, client device, exact error and
redacted logs for failures. Do not upload config files containing credentials.

## Later distribution/offline design (proposal, not implemented)

Use a component resolver with this order: verified local installation, explicitly
provided offline bundle, optional trusted organizational mirror, explicit vendor
provisioning fallback. Preserve signed manifest provenance, expected platform and
hashes; never trust a local executable merely because its filename matches.
A mirror remains a network dependency. Only having the components already local
removes download traffic. Unchanged Rust binaries still require authentic,
hardware-bound, unexpired entitlement; moving files cannot remove that requirement.

The user explicitly approved bundling the unmodified vendor downloads in this
project's test artifacts. Their presence is not a claim that the fork owns or can
relicense the proprietary code. This does not implement a permanent public mirror
or rebuild source we do not have. Tokens are not persisted in CI or included in
any artifact. The offline ZIP supplies files, not an offline or transferable
license. Each target machine still needs its own valid entitlement.

A fully offline implementation needs replacement/removal of licensing dependencies
in components we own or a separately authorized offline-capable backend. The
network inventory in [NETWORK_AUDIT.md](NETWORK_AUDIT.md) identifies those boundaries.
