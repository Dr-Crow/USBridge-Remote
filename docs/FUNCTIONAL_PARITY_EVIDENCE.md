# Functional replacement evidence, 2026-10-11

This is a bounded evidence inventory, not a complete vendor feature list. The
agent is substantially based on available upstream source; the separate
streamer and USB broker are independent source implementations. Having source,
compiling on a platform, or passing many CI jobs does not establish equivalent
functionality to an original executable.

The current agent planning estimate of **70–80% integration** is a subjective
scope estimate with substantial uncertainty, not measured feature parity. It
must not be updated by counting source lines, tests, fuzz cases or green jobs.
The rows below have no complete denominator or agreed weights. Before reporting
measured parity, enumerate the user workflows and platforms, agree their
importance, and run the same fixtures against exact original and replacement
binary hashes. Unknown behavior remains unknown; intentional security changes
must be assessed separately from compatibility regressions.

## Status vocabulary

- **implemented**: source and focused tests exist; no implied interoperability.
- **interoperability-tested**: an independent client/protocol peer completed the
  named scenario. This is not necessarily an original-binary comparison.
- **differential-verified**: the same controlled scenario was run against exact
  original and replacement binaries and the relevant outputs were compared.
- **hardware-verified**: the named real hardware/OS device path was exercised.
- **missing**: required end-to-end behavior is absent or not yet verified here.

These labels can coexist. No row below earns `differential-verified` merely
because its code resembles upstream or a synthetic peer matches a wire format.

## Agent and component-integration matrix

| Workflow / platform | Original baseline | Original behavior evidence | Our behavior and exact evidence | Status and remaining boundary | Intentional security difference |
|---|---|---|---|---|---|
| Agent startup/settings, Windows, Linux, macOS | Upstream [3.0.114 source pin 8524b84](https://github.com/USBridge-Technologies/USBridge-Remote/tree/8524b84bb06f17a97dcee96f8a2b4ac798cea310/agent) | Public source is available. No systematic original GUI binary replay suite has been run. | Agent-only upstream integration plus ownership fixes at [db6fb29](https://github.com/Dr-Crow/USBridge-Remote/commit/db6fb29fc545d26e32b7ef358f5e13fbbca58e30): native Windows/Linux/macOS and three web/Docker gates pass, jobs 501–506. [Upstream integration scope](UPSTREAM_3_0_114_AGENT_INTEGRATION.md). | implemented; native build/headless/operator-TLS checks do not prove every settings action or full GUI session. Windows test exclusions remain documented. | Strict local policy blocks automatic enrollment/provisioning and external services unless explicitly configured/authorized. |
| Local TLS, component pinning and offline policy | Same agent source; stock component v0.3.131 pins in [pinned.go](../agent/internal/localruntime/pinned.go) | Stock archives were inventoried and hash-verified. Exact original behavior under all offline/network faults is unknown. | Hash-bound manifests, operator TLS and strict request-boundary tests; [policy](RUNTIME_NETWORK_POLICY.md), [TLS](LOCAL_TLS.md), [network audit](NETWORK_AUDIT.md). | implemented, interoperability-tested for named TLS/local fixtures; no exhaustive zero-callback or vendor differential proof. | No certificate-error bypass, fabricated entitlement, silent account enrollment, or arbitrary unpinned binary replacement. |
| HTTP/HTTPS and browser USB relay lifetime | Upstream agent pin above | Source review establishes earlier unowned listener/worker paths; original binary failure frequency is not measured. | Stable db6 includes owned bridge, HTTP generations and route worker joins. [Bridge](OWNED_BROWSER_USB_BRIDGE.md), [HTTP](OWNED_HTTP_SERVERS.md), [WebSocket workers](OWNED_WEBSOCKET_WORKERS.md). | implemented with loopback/race/native tests; clipboard callback leases, some hijacked workers and other background services are still incomplete. | Failed or uncertain retirement is not reported as a clean reusable owner; short writes and substantive errors are retained. |
| Linux settings → local preview → visible renderer | Existing frozen source-streamer pin 2e07af3484369bc68bc8091d5a04969867eff0f9; agent [6bea3ef](https://github.com/Dr-Crow/USBridge-Remote/commit/6bea3ef055d7b42bd33245334e4c7926cae39ece) | Original streamer desktop behavior is not compared by this fixture. | Linux job479: four actual settings/engine lifecycles, 37 pixel samples, baseline restoration and isolated-container cleanup; source-free receipt SHA256 `3203f7878ce0149562aeeb9d2f4fb424a471dbe494cb034c3c64795c51a8abea`. | interoperability-tested for the named local generated-display profile; no broad desktop, GPU, WAN or stock parity claim. | Explicit local capture approval, fresh session/key, one lease, view-only and synthesized silence. |
| Windows native visible preview | Same frozen source and agent 6bea3ef | No original binary differential or real-desktop comparison in this fixture. | Job480: three CLI-generated lifecycles, 39 owned-window samples, 393 source frames and natural cleanup; media receipt SHA256 `ccdfaab93612e04bc9793cc30dc52183a7e387831fcb9184936ded4045f24108`. | interoperability-tested CLI fixture. **Missing:** ordinary non-elevated Manager/install/settings path; it remains gated on native proof and the exact OS-module trust closure. | Exact executable/DLL identity, strict owned-window sampling, no real-desktop or input permission inferred from generated pixels. |
| Browser secure WebTransport | Upstream 3.0.114 client source; no verified stock v0.3.131 server capability | Public client code was inspected. Actual current vendor transport behavior/performance is unknown. | Separate [4eba6c9](https://github.com/Dr-Crow/USBridge-Remote/commit/4eba6c9255c8a2e0208740755c9dcd72f0cd9862) branch: static CSP-safe modules, bounded parsers and joined session/stream ownership; Chromium524 has 12 WebCodecs frames, 110,592 checked pixels and 11 changes. Receipt SHA256 `1e2b5e72764a301dfa7dbc14f82c7e9cb71430c10674345c23c4074398eb82a3`. | implemented; browser synthetic decoding verified. That public receipt has `real_quic:false`. Private real-QUIC integration is a separate pending gate; production UI remains unavailable. | No Function/eval loader, no unsafe-eval CSP, bounded one-use scoped ticket, no silent trust/auth fallback. Leaf-hash authentication is an explicitly approved disposable test fixture, not a production discovery mode. |
| Reconstructed broker supervision | Stock USB broker v0.3.131 pinned archive; reconstructed source-broker-v1 contract | Static binary/protocol research exists; this agent matrix does not establish all physical device classes or original attach behavior. | Agent hash/manifest supervision plus actual mTLS transfer/cancel/idle-disconnect/capacity fixtures in the private broker project. | interoperability-tested portable session boundary; **missing here:** physical Windows USB attachment, broad device compatibility and a same-fixture original-vs-rebuilt device suite. | Explicit device authority/consent; portable wire-session readiness does not claim `os_attached:true`. |
| Webcam, microphone, KVM setup, remote/WAN and broad hardware combinations | Upstream 3.0.114 public UI/code; proprietary runtime support varies | Presence of code or static binary strings is not runtime evidence. No complete current stock feature inventory has been validated. | Some upstream paths are retained, and independent low-risk API guards are tested. There is no comprehensive reconstructed-component end-to-end proof for these combinations. | missing as replacement acceptance; hardware-verified must be earned per device/OS/profile, not inferred from virtual fixtures. | Capture/input/device consent, strict CSP and authenticated exact destinations remain requirements even where stock defaults differ. |

Latest read-only upstream check at 15:02 UTC found head
[22b9fa4](https://github.com/USBridge-Technologies/USBridge-Remote/commit/22b9fa4b6bcca75dc5d5211b46974952c5aecd5e).
The three commits after 8524b84 change a USB-setup wait from 25 to 75 to 40 and
back to 25 seconds; the net source difference is commentary. Latest release
remains 3.0.114. This finding does not establish runtime behavior of a newly
published vendor binary.

## How parity will be demonstrated

For each prioritized user workflow, retain: exact original/rebuilt executable
hashes and source/dependency pins; identical isolated inputs and permissions;
observable output/protocol/frame/device outcomes; failure/cancellation/reconnect
behavior; cleanup and performance measurements; and deliberate policy
variations. Run both binaries with the same fixture when safely available.
Record unavailable original artifacts or unknown behavior explicitly. Public
client interoperability and model tests remain useful evidence, but cannot
substitute for that comparison or for real hardware acceptance.

Private component code and binary delivery stay in their approved private
repositories. This public document contains only bounded status and evidence
identifiers, not those components or credentials.
