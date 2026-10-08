# Source LAN hosting

The source container serves the WASM client, its complete local model/ORT
assets, public runtime configuration and a read-only component mirror. Capture,
encoding, input and USB remain in native host processes. It does not issue
entitlements, replace a license server, relay sessions or install drivers.

## Build and operator configuration

`deploy/lan/Dockerfile` builds Go 1.26.6 source using the pinned official
linux/amd64 image, then copies the server and web assets into a nonroot scratch
image. Original proprietary components are never copied into the image.

Prepare two directories outside the checkout:

- TLS directory: `server.crt` and `server.key`, with a certificate covering the
  numeric private IP used in the browser. The container user 65532 must be able
  to read these files. Use appropriate ownership/group access; avoid making a
  production private key world-readable.
- Component directory: `manifest.json` and exactly the files declared by the
  [local component schema](LOCAL_COMPONENTS.md). The agent must be configured
  with an independently verified SHA-256 of the manifest. The host validates
  the schema and hashes every component response; it serves no undeclared file.

For example, set `USBRIDGE_TLS_DIR`, `USBRIDGE_COMPONENT_DIR`,
`USBRIDGE_AGENT_ORIGINS=https://192.168.1.8:8443` and an explicit
`USBRIDGE_LAN_BIND=192.168.1.5`, then run:

```sh
docker compose -f deploy/lan/compose.yaml build
docker compose -f deploy/lan/compose.yaml up -d
```

The default bind is loopback. Compose uses read-only mounts/root filesystem,
drops capabilities, forbids privilege escalation and creates an internal
network. Set agent origins to the actual native agent HTTPS ports, not the
streamer's unauthenticated internal endpoints. Pairing/HMAC authentication is
unchanged. Certificate trust must already be established in the browser/agent;
this setup never installs trust, disables certificate verification or changes
host firewall settings. These commands are operator instructions, not a claim
that this checkout has been deployed.

Open `https://192.168.1.5:8443/`. The host always generates
`runtime-config.js` with `strictLAN: true`; configure the agent mirror as
`https://192.168.1.5:8443/components/`, the manifest hash and, if required, the
explicit client-scoped CA file. Restart the host after replacing a manifest;
its validated manifest snapshot remains immutable during each server run.

## Browser and streamer policy

The strict browser accepts literal private/link-local/loopback IP destinations,
requires HTTPS/WSS except loopback development, rejects public DNS/IP requests
before fetch/WebSocket transport, and rejects HTTP redirects. Its RTC
constructor and later configuration forbid ICE servers. The Go client uses an
empty ICE list and does not try hosted signaling fallback in strict mode.
The hosting CSP restricts requests to itself and the declared local agent
origins, including WSS. Missing browser runtime configuration fails closed;
the ordinary source static bundle explicitly defaults to normal mode.
Bootstrap code is external so the host needs no inline-script permission.

Strict agent streamer arguments omit the cached vendor TURN credential path
and explicitly pass `--webrtc-ice-servers=`. Direct launches filter inherited
streamer ICE/TURN variables; Windows session launches override the documented
ICE/TURN environment variables. Normal-mode arguments retain their previous
behavior. Tests verify argument/environment construction, not the proprietary
parser or subprocess network behavior. No compatible pinned binary is available
on this Intel Mac to establish empty-value parser acceptance; this remains an
acceptance gate for each runtime/platform.

Local-runtime metadata is separate from the vendor tariff. A verified prepared
RustShine research runtime gets a local label and RustShine mouse-coordinate
mapping, while codec/device availability still comes from live backend status.
Preparation does not prove capture, USB or 4:4:4 operation. The current WASM
adapter does not implement native 4:4:4/HDR negotiation.

## Verification and limits

The LAN server tests HTTPS asset MIME types, CSP/configuration, declared mirror
files, tampering, symlinks, unsafe origins and method/path restrictions. Browser
API mocks exercise public request denial, empty ICE and redirects. CircleCI's
`lan-host` job builds the actual image and checks TLS/assets/mirror behavior on
an internal network with disposable fixture certificates; it publishes logs,
image identity and commit provenance, not an image or release. No fixture key
is retained in artifacts.

At the initial commit, the Mac's Colima engine remains stopped and Docker
build/run awaits that exact commit's CI. Real browser empty-cache loading,
streamer parser acceptance, restart/expiry behavior, physical streaming/USB and
packet capture with public egress blocked remain required before claiming a
working offline system. Browser guards and CSP are request boundaries, not an
OS firewall or evidence about proprietary child-process traffic.
