# USBridge LAN-only deployment scope

Status: proposal, 8 October 2026. The existing 0d5f6cf agent is NOT yet LAN-only.
The persistent runtime setup added on this branch is stage A only; it does not
implement the strict-LAN policy proposed below.

## Required outcome

After installation from an offline bundle or an explicitly configured LAN mirror,
Windows/macOS/Linux hosts must start, pair, stream and reconnect with WAN blocked.
No USBridge entitlement, account, DNS, certificate, signaling or relay service may
be required or contacted. Local pairing, authentication and encrypted transport stay.

## Deployment layout

- Native host agent: desktop capture, codecs, local streamer, USB broker and drivers.
  Docker on another host cannot capture a Windows/Mac desktop or attach its devices
  by itself. Linux capture in containers is a separate device/permission project.
- Docker Compose server: static web-client assets (if proved portable), a read-only
  component repository, local configuration/discovery API, and HTTPS reverse proxy.
- Optional local signaling service: only if the web client's actual protocol needs
  centralized signaling. Prefer direct authenticated client-to-agent signaling when
  already supported. Do not invent a protocol before inspecting the client.
- Optional self-hosted TURN: for network topologies that need relay. Same-LAN hosts
  may work with host ICE candidates; test this, including browser mDNS candidates.
  Never depend on a public STUN/TURN default in strict mode.

## Agent configuration and component resolution

Add explicit strict-LAN policy, independent of whether the user is signed in.
Policy is evaluated before network service constructors and every background task.
Use component source order: validated installed copy; selected offline bundle;
explicit LAN mirror. No public-internet fallback in strict mode. A separate online
provisioning mode may permit vendor downloads with genuine credentials.

Versioned manifests contain OS/architecture, component version, expected hashes,
compatibility profile and dependencies. Verify files before extraction or launch;
reject traversal/symlink escapes and mismatched architecture. Preserve original
archives and use atomic staging/activation, last-known-good rollback and useful
errors. Do not use CircleCI artifact storage as a permanent release channel.
Local mirror authentication must not be a substitute license service.

The existing local-runtime mechanism produces modified copies of pinned binaries
and local claims, with no vendor private key. Audit every watchdog so vendor token
expiry does not later disable a working local runtime. Local claim renewal must
survive long-running sessions, restart, clock skew and expiration without network.
HWID required by the binary may remain locally for compatibility, but must not be
transmitted. Do not equate deleting an HWID field with removing a dependency.

## Disable or replace in strict mode

- Vendor entitlement refresh, purchases, account login/poll/rebinding.
- Vendor DNS and certificate enrollment/renewal.
- Hosted signal relay and TURN credentials.
- Embedded Tailscale startup/control-plane/remote logging, not merely UI sign-out.
- Agent/component public update jobs, online driver downloads and benchmarks.
- Remote web links/assets, fonts, service workers, analytics and crash uploads.
- Any third-party calls discovered inside the proprietary components.

Preserve local HTTPS. Supply certificates through the local administrator's PKI or
an explicit onboarding workflow; do not automatically disable certificate checks.
Keep local pairing and permissions. Browser secure-context requirements must be
validated for the chosen hostnames, origins and browser APIs.

## Web/WASM investigation

The vendor page and WASM endpoint returned HTTP 403 to the downloader. The user then
provided a Drive copy which downloaded successfully: 55,555,653 bytes, WASM magic
and version 1 verified, SHA256
`aafebfe9902bdc964dc5a544aef5b9f75b213ce6510b5237fd84db516dc239f5`.
This identifies that supplied artifact, not a reproduced build or upstream signature.

The public repository includes a preferable source-based route:

- `client/cmd/wasm/main.go`: Go/Fyne browser entry point.
- `client/web/index.html`: HTML loader using relative app.wasm and wasm_exec.js.
- `client/scripts/build_web.sh`: GOOS=js GOARCH=wasm build, matching Go loader,
  HTML staging, ONNX model staging and local runtime assets.
- `client/internal/webrtcweb/client_wasm.go`: public Google STUN default at
  `stun:stun.l.google.com:19302`, plus direct offer and hosted fallback logic.
- `client/internal/webrtcweb/signal_relay.go`: vendor relay base URL and fallback.

Plan: build our own client and matched loader instead of binary-patching WASM.
Replace hard-coded ICE configuration with an explicit local policy (empty servers
for same-LAN host candidates or administrator-supplied local STUN/TURN). Disable
vendor relay fallback entirely in strict mode. Audit the remaining browser call
sites and bundled third-party assets. Configure authenticated local signaling,
allowed origins, trusted HTTPS and browser private-network permissions as needed.
Serve WASM with the correct MIME type and validate cross-origin isolation headers
against actual SharedArrayBuffer/ONNX requirements. Bundle model/runtime assets or
explicitly disable optional AI features, avoiding hidden CDN/model downloads.

Raw WASM string candidates corroborate vendor/STUN strings but are NOT proof of
executed network calls. No browser-to-agent LAN stream has yet been tested.

## Proof required before declaring offline support

1. Clean install from bundle with no vendor token/account and WAN already blocked.
2. Agent/broker/streamer start, pair and stream on an isolated LAN.
3. Session reconnect, restart/reboot, prolonged idle and claims expiry/renewal.
4. Video codec/chroma verification and physical tablet/USB acceptance.
5. Web-client full reload with empty browser cache and no external assets.
6. Capture DNS/TCP/UDP attempts for agent, child processes, browser and containers.
   Working despite failed callbacks is insufficient: strict mode must make no
   unauthorized outbound attempt. Test against egress firewall as a backstop.
7. Missing/corrupt bundles, absent mirror, incompatible versions, time changes,
   cancellation and rollback fail clearly with no silent cloud fallback.

## Delivery stages

A. One-click persistent runtime selection and honest status UI (implemented on this
   branch; hardware and manual launch acceptance remain pending).
B. Local bundle/mirror resolver and strict service-start/network policy.
C. Browser client portability and Compose packaging, after obtaining client assets.
D. Independent binary network audit and offline hardware acceptance matrix.

No claim yet of complete binary-source recovery, update-proof runtime modification,
full offline operation, or working locally hosted web streaming. Windows/Linux x64
and macOS ARM64 component pairs were present in the inspected manifest; macOS Intel
needs another compatible backend. Windows/Linux ARM64 are not existing packaged
targets in this fork. License/redistribution provenance must accompany any bundles.
