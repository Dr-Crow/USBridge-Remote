# Strict-LAN engineering checkpoint

8 October 2026. This checkpoint describes source implementation and local
verification; it is not physical offline acceptance or a deployment claim.

## Completed phases

- Published `28ee5129`: strict agent request boundaries and cloud watchdog /
  Tailscale suppression.
- Published `0a78ead824ddc62e196ac2f24192cd75480e16eb`: pinned local component
  directory/ZIP/LAN-mirror resolution, verified staging and rollback. Root
  verified Windows #50, macOS #48 and web #51 passed; Linux #49 failed on the
  pre-existing clipboard pending-before-file ordering race.
- Local commit `c2a17930`: serialize clipboard callback registration and initial
  pending announcement/resync. Fifty race-enabled repetitions passed, broader
  clipboard transport tests passed twice, and the full clipboard package passed.
- Local commit `f029cfd3`: optional local-runtime metadata, separate from vendor
  tariff, with prepared runtime labeling and correct mouse-coordinate mapping.
  Missing/free/expired vendor status does not hide prepared local metadata;
  unprepared/inactive runtime stays unpromoted. Live codec/device status remains
  authoritative. App consent/runtime/codec regressions passed.
- Browser/streamer phase: strict browser rejects public fetch/WebSocket requests
  and redirects, keeps RTC ICE empty and disables hosted signaling fallback.
  Streamer arguments exclude cached vendor TURN files and explicitly override
  public ICE defaults; inherited documented ICE/TURN variables are overridden.
- Source-host phase: nonroot source-built HTTPS web/mirror container, read-only
  mounts and internal Compose network. Requires explicit TLS cert/key and private
  agent origins. Serves only declared, rehashed component files. No entitlement
  issuer, capture/USB drivers, relay, production credentials or proprietary
  components are copied into the image. See LAN_HOSTING.md.

## Local verification

Complete source-WASM pipeline passed module verification, native and WASM tests,
WASM vet/build, JS syntax/browser policy tests, Node WebAssembly compilation and
packaging checks. The final packaging check includes all 19 runtime assets,
including the extracted bootstrap. Eight packaging tests passed, including a
missing-bootstrap regression. The local ZIP was built from f029cfd3 with later
uncommitted phase changes; its provenance reports a modified checkout and must
not be represented as an exact clean-commit CI artifact.

Native and WASM capability/model/WebRTC tests passed. Agent strict ICE and
metadata tests passed with the race detector. LAN host TLS/asset/CSP/mirror/
tampering/path/origin tests passed with race checking; vet passed. Shell syntax,
shellcheck, CircleCI YAML and Compose configuration validation passed.

## CI and acceptance gates

The Mac's Docker context is Colima; the engine remains stopped. Do not start it
just for these checks. The new CircleCI lan-host job builds the actual image and
checks TLS, assets, strict config, mirror allowlisting, missing-TLS rejection and
an internal-network WAN denial using disposable fixture credentials. Image and
release publishing are not part of this checkpoint. Results must be tied to the
new pushed commit; no new Docker CI success is established yet.

No compatible pinned streamer binary is available on this Intel Mac. Empty ICE
CLI parser acceptance is still required for each compatible runtime/platform;
argument tests are not executable/network evidence. No original runtime process
was launched. Real browser empty-cache reload, TLS trust, restart/expiry behavior,
physical capture/encoding/USB and packet capture with WAN blocked remain untested.
The WASM adapter does not implement native 4:4:4/HDR negotiation.

Jim directly authorized edits, commits and pushes in this task, superseding the
original read-only fork restriction. Use Dr-Crow only, scoped commit identity and
existing personal SSH key. No merges, releases, installs, global account changes,
OS trust/firewall changes or deployment occurred. Earlier approval-review denials
were resolved by that direct authorization. Two subsequent tool calls were
reported aborted by user, without an explicit cancellation message or detailed
executor reason; their git staging did not execute. Preserve work across such
interruptions and report any actual stop instruction.
