# Agent outbound-network inventory and offline proposal

8 October 2026. Static review of the agent at upstream
`6f9d27223b310e8c6726b458739c8ae86a5b52dd`, with the provisioning changes on this
branch noted below. This is not a packet capture or an exhaustive audit of compiled
third-party libraries and closed Rust components. A functional call can still
transmit identifying information; not every outbound request is analytics.

## First-party calls

| Purpose/destination | Data sent | Trigger/cadence | Control and offline effect |
|---|---|---|---|
| Vendor `/v1/desktop-license/refresh` on `usbridge-entitlement.fatkulinamir80.workers.dev` | Stable derived `hw_id`; normal HTTP metadata/source IP | Startup, explicit component setup, normally 6h refresh; branch retries failures after 5m | Required to issue/renew current signed entitlement. Valid cached tokens verify locally; no extra offline grace is implemented. |
| Vendor `/v1/desktop-billing/checkout` | `hw_id`, requested tier | Explicit purchase in retained compatibility/account paths | Not needed for free provisioning. Backend tiles no longer initiate purchase. External billing records are not changed by this fork. |
| Vendor `/v1/download/rustshine` and `/v1/download/usb-broker`, then GitHub release-asset host returned by vendor | Genuine token in Authorization header to vendor; platform query. Temporary signed URL to asset host | Explicit consent/selection, interrupted setup retry, component update checks | Can use already staged binaries; missing files require connectivity. Archive SHA256 and available manifest verification must remain. |
| `USBridge-Technologies/Streamers-Forks` GitHub release manifests/assets | Normal HTTPS request metadata, no entitlement header in this path | Sunshine/Punktfunk first provisioning and update jobs | Installed backend can run without a new download. Separate from license service. |
| Agent's upstream GitHub release manifest/signature and update assets | Platform/version evaluated locally; HTTP User-Agent and network metadata | Agent startup check, user-approved GUI update; headless path can apply updates | Test fork must avoid being overwritten by upstream updates. An independent fork update channel needs its own reviewed release process; do not turn off signature verification. |
| Vendor `/v1/device/dns` | Stable `hw_id` **and private/local IP address** | Local IP polled every 3s; registration at change/start and about every 5m | Gated by HTTPS configuration. Disabling HTTPS to avoid this also changes local transport behavior, so not an adequate fine-grained privacy control. Offline falls back to locally generated certificate; browser trust may fail. |
| Vendor `/v1/device/cert` | `hw_id`, base64 CSR for assigned hostname | Certificate setup/renewal; pending issuance retry about 1m | CSR private key stays on device in this implementation. Vendor/issuer can learn assigned hostname; certificate transparency implications need review. |
| Vendor `/v1/webrtc/turn-credentials` | `hw_id`; no entitlement Bearer header in public function | Staged streamer, valid Pro/Enterprise status; immediate then 50m | Network errors retain existing timed credentials; explicit `not_pro` removes them. A local/direct stream and vendor TURN relay are different connectivity paths. |
| Vendor `wss:/v1/webrtc/signal-relay/connect` | `hw_id` in query; session SDP/signaling on connection | Enabled/staged/paid-valid conditions; ineligible retry 10m, connection-error retry 15s | `webrtc_signal_relay_enabled=false` opts out. Relay transports signaling, not itself the media stream. Direct LAN connectivity is separate. |
| Vendor account login/start and login/poll | OS, architecture, CPU/GPU model, core count, total RAM; device code polling | Explicit account sign-in; polling about 2s while pending | Coarse device inventory is explicitly collected here. Account sign-in is separate from automatic free hardware entitlement. Avoid account sign-in if not needed. |
| Vendor `/manage/api/licenses?kind=desktop`, `/manage/api/rebind` | Account Bearer token; license identifier/current hardware ID for rebind | Account/license UI refresh or explicit rebind | Optional account management. Do not include account credentials in logs/artifacts. |
| Benchmark sample on `raw.githubusercontent.com/bower-media-samples/.../video.mp4` | Normal HTTP metadata | Benchmark content fetch when needed | Optional test content; stage local test media for an offline benchmark. |
| Browser links: `web.usbridge.io`, `billing.usbridge.io`, Stripe checkout, docs, USB driver releases | Browser URL/request metadata and whatever user enters | Explicit UI action; some account flows poll afterwards | Website/provider behavior is outside this agent-source audit. USB driver installation is separate from broker licensing. |

Sources: [entitlement client](../agent/internal/entitlement/client.go),
[download/staging](../agent/internal/entitlement/download.go),
[app watchdogs](../agent/internal/app/app.go),
[signal relay](../agent/internal/app/webrtc_signal_relay.go),
[device DNS/CSR](../agent/internal/devicecert/client.go),
[account calls](../agent/internal/account/account.go),
[account hardware inventory](../agent/internal/account/hwinfo.go),
[open streamer releases](../agent/internal/forkrelease/punktfunk.go),
[self update](../agent/internal/update/manifest.go),
[benchmark](../agent/internal/benchvideo/benchvideo.go).

## Hardware identifier

[`hwid.Get`](../agent/internal/hwid/hwid.go) hashes a fixed purpose/version prefix,
source label and OS identifier using SHA256. Windows reads MachineGuid; macOS uses
IOPlatformUUID; Linux uses machine-id. The resulting value is stable and linkable,
not an anonymous random session ID or a secret salt. Hashing avoids transmitting
the raw OS value in these calls, but does not eliminate per-device tracking.
Deleting/changing it now can break backend token issuance, existing token matching
and runtime checks in the unchanged streamer/broker. Free is still a signed,
hardware-bound vendor tier.

## Dependencies, local traffic and limits

- Embedded Tailscale `tsnet` starts network machinery; control-plane login,
  coordination, peer discovery, relay and diagnostic logging are dependency-level
  concerns. The supplied runtime log showed contact with
  `controlplane.tailscale.com`, node registration and a login flow. The wrapper's
  local logging callbacks do **not** prove remote telemetry is disabled. A full
  dependency configuration review and traffic capture are required to enumerate
  dynamic DERP/STUN/logging destinations and payloads. See
  [wrapper](../agent/internal/tailscale/service.go). A UI sign-out is not proof of
  zero outbound traffic; add/test a genuine startup-disable policy later.
- The `8.8.8.8:80` UDP route probe in
  [localip.go](../agent/internal/netutil/localip.go) calls Dial to select a local
  source address; no application payload is written there. Do not mislabel it as
  sending analytics, and do not claim zero packets without measurement.
- Loopback streamer admin APIs, local broker control socket, admin IPC and
  authenticated client-to-agent calls are not vendor telemetry. LAN discovery,
  mDNS and peer traffic should still appear in a complete network test.
- Static strings in six Rust builds show entitlement-related code. They cannot
  establish whether those binaries make additional network calls or disclose
  analytics. No packet capture, decompilation or proprietary runtime network test
  has been performed.

## Phased offline/privacy plan

1. Keep this test build's provisioning functional and record a clean launch,
   selected-backend launch, idle, USB enable and active-session traffic capture.
   Classify DNS, HTTPS, WebSocket, UDP and child-process destinations separately.
2. Add explicit independent policies for vendor certificate/DNS registration,
   Tailscale startup/diagnostics, agent/component update checking, relay service,
   account functions and benchmark media. Defaults and migrations need review;
   turning off HTTPS is not a substitute for turning off certificate registration.
3. Implement verified local component paths/offline bundle resolution before
   optional mirrors/vendor fallback. Confirm licensing terms before redistributing
   prebuilt proprietary components. No token/private key in bundles.
4. Validate with egress blocked, both valid and expired tokens, reboot/restart,
   reconnect and consent cancellation. Keep local authentication/TLS/security.
5. For permanent offline operation without hardware identity/expiring license,
   use independently maintainable offline-capable components or obtain a supported
   vendor offline entitlement. A mirror alone does not meet this goal.
