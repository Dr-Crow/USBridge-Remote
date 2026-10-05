# TODO: add Punktfunk as a third streamhost backend

**Status: GameStream-plane backend implemented (`punktfunk_backend.go`,
`punktfunk_pairing.go`, `punktfunk_codec.go`, `punktfunk_config.go`,
`punktfunk_devices.go`, `punktfunk_process_{windows,other}.go`), unit-tested
against a fake server mirroring punktfunk's real documented wire protocol
(`punktfunk_backend_test.go`, 8 tests, pass under `-race` on darwin; package
also cross-compiles clean for windows and linux/CGO_ENABLED=0). The native
punktfunk/1 plane and app-level backend-selection UI wiring are explicitly
deferred -- see "What's done" / "What's still open" below.**

**Field-tested 2026-10-02 against a real `punktfunk-host` 0.42.0** (built from
source on Linux, KDE Wayland, NVIDIA) -- see "Field test" below. Still not
done: a full video session through the USBridge client, and the native plane.

## What

Add `crates/punktfunk-host` (cloned from the private `git.unom.io/unom/punktfunk`
into `/Users/amir/Projects/usbridge/punktfunk`) as a third `streamhost.Backend`
implementation in the Go agent, alongside the existing `sunshineBackend`
(`sunshine_backend.go`) and `rustshineBackend` (`rustshine_backend.go`).

## What Punktfunk actually is

Not a Sunshine/Moonlight fork — a separate, independently-architected streaming
product with its own native protocol plus an optional Moonlight-compatible
plane, both served by one process:

- `punktfunk-host serve` always speaks `punktfunk/1` (its own QUIC-controlled
  protocol: SPAKE2 pairing, UDP data, GF(2^16) Leopard FEC, AES-GCM).
- `punktfunk-host serve --gamestream` additionally speaks GameStream
  (nvhttp/RTSP/ENet) for stock Moonlight clients, same as Sunshine/rustshine.
- Management API: axum over HTTPS, versioned under `/api/v1`, default
  `127.0.0.1:47990` (`--mgmt-bind` / `PUNKTFUNK_MGMT_BIND` to move it). Two
  credential lanes: a paired client cert for the read-only LAN surface, and a
  bearer token (`<config>/mgmt-token`, admin-only, loopback-only) for
  everything else, where `<config>` is `~/.config/punktfunk` (Linux) /
  `%ProgramData%\punktfunk` (Windows). The host writes the bound URL to
  `<config>/mgmt-endpoint` on every start (`PUNKTFUNK_MGMT_URL=https://127.0.0.1:<port>`)
  -- read that instead of assuming 47990.
- Full API surface: `api/openapi.json` in the punktfunk repo (96 routes).
  `punktfunk-host openapi` also prints it from the binary.

## Endpoints relevant to `streamhost.Backend`

Confirmed directly from `api/openapi.json` (not guessed):

| Need (Backend interface, `backend.go`) | Punktfunk endpoint | Notes |
|---|---|---|
| `WaitReady` | `GET /api/v1/health` | Unauthenticated (`require_auth` exempts it). Returns `{status, version, abi_version}`. |
| `CodecProbe.SessionActive` | `GET /api/v1/status` -> `RuntimeStatus.active_sessions` (int, can exceed 1 -- native plane allows concurrent sessions) or `.sessions`/`.games` | Needs bearer token (`mgmt-token` file), **not** HTTP Basic like Sunshine/rustshine -- see auth mismatch below. |
| `PairingAPI.ListClients`/pairing flow | `GET /api/v1/pair` -> `PairingStatus{pin_pending, pending: [PendingCeremony]}` | **Different shape than Sunshine/rustshine**: Punktfunk's `pending` is a list of *ceremonies* (fingerprint/uniqueid/peer_ip), not already-paired clients. Paired-client counts only surface as integers (`paired_clients`, `native_paired_clients`) in `RuntimeStatus`, not a listable array in this spec slice -- need to check the rest of the OpenAPI doc for an actual paired-clients list endpoint before assuming one exists. |
| `PairingAPI.SubmitPIN` | `POST /api/v1/pair/pin` body `SubmitPin{pin, uniqueid, fingerprint, peer_ip, label?}` | **Must echo the exact ceremony identity from `GET /pair`'s `pending[]`**, not just the PIN -- structurally different from Sunshine's "POST the newest pairing_id" and rustshine's flow. A naive port of either existing `SubmitPIN` implementation will not compile against this shape. |
| Identity | n/a | `DisplayName()` -> e.g. `"Punktfunk"`. |

## Why this is parked, not started

1. **Auth mismatch**: Sunshine/rustshine's `PairingAPI.AdminUser/AdminPass` assume
   HTTP Basic auth; Punktfunk uses a bearer token read from a local file
   (`<config>/mgmt-token`) plus mTLS client certs for the LAN lane. The
   `Backend` interface's `AdminUser()/AdminPass()` pair doesn't fit this model
   -- needs either a new interface method/variant, or a translation layer in
   the punktfunk backend that reads the token file at startup and treats it as
   the "password" with a fixed sentinel "user" (ugly, needs a real decision,
   not a workaround).
2. **Pairing ceremony shape is structurally different** (see table above) --
   `SubmitPIN(adminPort int, pin string) error`'s signature doesn't carry
   enough information (no fingerprint/uniqueid/peer_ip) to call Punktfunk's
   `POST /pair/pin` correctly. Either the interface needs to grow, or the
   punktfunk backend needs to itself call `GET /pair` immediately before
   submitting to resolve the one pending ceremony (mirrors how
   `sunshineBackend.SubmitPIN` already polls `GET /api/pin` for the newest
   pending request, so there's a working precedent to follow -- see
   `sunshine_pairing.go`'s `pendingPairingWait` poll loop).
3. **Process lifecycle unresearched**: how the agent should invoke
   `punktfunk-host serve --gamestream`, what flags/env vars pin the admin
   port, config dir, capture backend, and output name (the `ConfigStore`
   interface's `SetCaptureMode`/`SetOutputName`/`SetExternalIP`/etc.) has not
   been looked at yet -- only the management API was researched this pass.
   Needs reading `crates/punktfunk-host/src/mgmt.rs`, the CLI arg parser, and
   `pf-host-config` (the settings registry) before any `Start()`/`ConfigStore`
   implementation is written.
4. **No session-active / codec-probe parity check yet**: `RuntimeStatus` has
   `active_sessions`/`sessions`/`games`, which looks sufficient for
   `SessionActive()`, but `CurrentVideoCodec()`/`SupportedVideoCodecs()`/
   `Color444Status()`/`HdrStatus()`/`VirtualDisplaySupported()` have no
   confirmed Punktfunk equivalent yet -- `RuntimeStatus`'s full schema (this
   pass only read the first ~40 lines of it) needs a full read, plus
   `/api/v1/stats/capture/status` which may be the right source for some of
   these.
## What's done (2026-10-01)

Scoped down to GameStream-only per explicit direction: native `punktfunk/1`
is phase 2, together with client-side changes, once the client supports it.

- **Lifecycle**: `Start`/`Stop`/`WaitReady`/`Pid`/`BinaryPath`/`Running` --
  spawns `punktfunk-host serve --gamestream --mgmt-bind 127.0.0.1:<adminPort>`,
  pins a freshly generated 32-byte hex bearer token via `PUNKTFUNK_MGMT_TOKEN`
  (confirmed env override from `mgmt_token.rs`) and the config directory via
  `PUNKTFUNK_CONFIG_DIR` (confirmed from management-api.md), so this backend
  never has to read punktfunk's own `mgmt-token` file back. `WaitReady` polls
  the confirmed-unauthenticated `GET /api/v1/health`. Process supervision
  (SIGTERM/SIGKILL, Windows Job Object, Linux Pdeathsig, orphan-by-name
  cleanup) mirrors `sunshineBackend` exactly, simplified (no AppImage
  staging, no Windows session-broker, no KMS capexec -- none confirmed
  necessary for Punktfunk yet).
- **Pairing**: `ListClients` (`GET /clients`, fingerprint used as
  `Client.UniqueID` since Punktfunk has no separate persisted uniqueid),
  `UnpairClient` (`DELETE /clients/{fingerprint}`), `SubmitPIN` (polls
  `GET /pair` for the newest pending ceremony, echoes its full
  uniqueid/fingerprint/peer_ip identity into `POST /pair/pin` -- the
  structural mismatch flagged below under "open questions" turned out to
  just need the same "poll until it appears" shape `sunshineBackend.SubmitPIN`
  already uses against `GET /api/pin`, not an interface change). Every call
  sends `Authorization: Bearer <token>`, confirmed from `api/openapi.json`
  as the auth model for every admin route except `/health`.
- **CodecProbe**: `SessionActive` reads `GET /api/v1/status`'s
  `RuntimeStatus.active_sessions` (live app state, not a log scrape -- same
  precedent as the earlier Sunshine/rustshine `SessionActive` fix).
  `VirtualDisplaySupported` is `true` (confirmed: `pf-vdisplay` creates a
  per-session virtual output on every supported compositor + the Windows
  IddCx driver). `CurrentVideoCodec`/`SupportedVideoCodecs`/
  `Color444Status`/`HdrStatus` are conservative stubs (no confirmed
  Punktfunk equivalent found this pass -- see "what's still open").
- **ConfigStore**: `ConfigPath`/`SetConfigKey`/`ConfigKey` wired to
  Punktfunk's real `settings set`/`host-settings.json` mechanism (confirmed
  CLI command). The five typed setters
  (`SetExternalIP`/`SetBindAddress`/`SetCaptureMode`/`SetAudioSink`/
  `SetOutputName`) are no-ops, deliberately -- their real `pf-host-config`
  setting IDs were never looked up, and a wrong guess could silently write
  the wrong setting. `LogPath` is `""` (Punktfunk's own internal log file
  path, distinct from this backend's stdout/stderr capture, isn't confirmed
  either).
- **CaptureDeviceLister**: stub (`nil`) -- `punktfunk-host list-monitors`
  exists but its output format wasn't parsed this pass.
- **NetworkPorts**: `Ports(basePort)` reuses `sunshineBackend.Ports`'s exact
  offset formula, confirmed correct against
  `crates/punktfunk-host/src/gamestream/mod.rs`'s real port constants
  (`HTTP_PORT=47989`, `HTTPS_PORT=47984=basePort-5`,
  `RTSP_PORT=48010=basePort+21`, `CONTROL_PORT=47999=basePort+10`) -- not
  assumed, verified to match byte-for-byte.
- **Tests**: `punktfunk_backend_test.go`, a fake HTTPS server reproducing
  the real bearer-auth + ceremony-echo wire shapes, 8 tests, clean under
  `go test -race`. Package builds clean for darwin (native), windows
  (`GOOS=windows`) and linux (`CGO_ENABLED=0 GOOS=linux` -- plain
  cross-compile fails on this dev machine for unrelated cgo-toolchain
  reasons, not this code).

## Field test (2026-10-02)

Driven through `streamhost.NewPunktfunk` itself, with the client's own
`moonlight.Client` as the Moonlight side, in private net+pid namespaces (the
GameStream ports are compile-time constants in punktfunk, so it can't run
next to another streamer; and Start/Stop's `killall`-by-name cleanup would
otherwise hit the box's live streamer).

Works: settings CLI before first start (`max_fps`, unknown id rejected);
Start + WaitReady (~0.3-0.5 s); SessionActive; ListClients; SubmitPIN with
and without a pending ceremony; the full PIN pairing from the client;
`/applist`, `/launch`, `/cancel` over the paired HTTPS port; pairing survives
a restart (config dir pinned under stateDir); UnpairClient; an external
SIGKILL fires onExit and the next Start recovers; mirroring a physical
monitor (`mirror-test`, 3840x2160 frames).

Found:

- **KDE needs a .desktop file naming the binary's exact path.** KWin only
  gives `zkde_screencast_unstable_v1` to a punktfunk-host whose executable
  path is the `Exec=` of an installed .desktop listing it in
  `X-KDE-Wayland-Interfaces` (punktfunk ships
  `packaging/linux/io.unom.Punktfunk.Host.desktop` for `/usr/bin`). Without
  it every session fails at capture. A user-level copy in
  `~/.local/share/applications` naming the real path works (after
  `kbuildsycoca6`). A binary inside an AppImage's per-run mount point can
  never match one, so bundling Punktfunk into the Linux AppImage won't work
  on KDE; it has to live at a stable path.
- **No KMS capture.** Punktfunk captures through the compositor only
  (PipeWire via KWin/Mutter/portal, wlroots capture). Nothing to map the
  agent's "kms" capture mode onto.
- **The GameStream ports can't be moved** (47984/47989/47998-48000/48010 are
  constants), so `Ports(basePort)` is only right for the default
  `sunshine_port`.
- `/host` reports `codecs` and `/display/monitors` the monitors and the pin
  -- the sources for `SupportedVideoCodecs` and a device list without the
  CLI.
- An unpaired client's `/applist` gets HTTP 200 with a GameStream error
  body, which the client's `GetAppList` reads as an empty list.

## Benchmark wiring (2026-10-02)

- `punktfunkBinaryPath`: bundled `<exeDir>/punktfunk/`, then
  `$USBRIDGE_PUNKTFUNK_HOST`, then `punktfunk-host` on PATH.
- `ListCaptureDevices` parses `list-monitors`; `SetOutputName` keeps a
  connector name in `<config>/usbridge-capture-monitor` and Start passes it
  as `PUNKTFUNK_CAPTURE_MONITOR` (mirror that monitor instead of a virtual
  display per session).
- `App.SetStreamBackend("punktfunk")` no longer force-pins a physical
  monitor when none is picked (removed `pinPunktfunkMonitor` -- it defeated
  the point of benchmarking Punktfunk's actual default path). With no
  `PUNKTFUNK_CAPTURE_MONITOR` set, no console display policy configured (a
  fresh managed config dir never has one) and no `PUNKTFUNK_COMPOSITOR` pin,
  punktfunk's own `effective_topology`/`resolve_topology`
  (`pf-vdisplay/src/lib.rs`) resolves `Auto` to `Exclusive` on Linux: the
  per-session virtual display becomes the *sole* display, physicals turn
  off for the session's duration. The benchmark's video player (started
  with no monitor target when the dialog's own monitor pick is empty) then
  has nowhere else to open but that one remaining display -- confirmed from
  `docs-site/.../virtual-displays.md`'s "Your monitors while streaming"
  table and the `Topology` enum doc comments in
  `crates/pf-vdisplay/src/vdisplay/policy.rs`, not guessed. Explicitly
  picking a monitor in the benchmark dialog still works exactly as before
  (`SetBenchMonitor` -> `applyBenchMonitor` -> `SetOutputName`), unaffected
  by this change.
- `BenchStreamBackends` lists it when a binary is found; the client's
  benchmark dialog shows the row only then.

## Agent GUI wiring (2026-10-02)

- `entitlement.Status.PunktfunkAvailable`; the streamer picker
  (`ui/protocol_picker.go`) shows a fourth tile, on a second row, only then.
- Saved as `PreferredBackend` like the other two and restored in `New()`
  when the binary is still there (else Sunshine).
- The client still sees `agent_protocol: "opensource"`; the picker has its
  own key (`protocolPunktfunk`).
- Not looked at: the Status card's streamer version line and the legacy
  license dropdown, which only know Sunshine and RustShine.

## What's still open

1. **A real video session through the USBridge client** (RTSP, video, audio,
   input) -- the benchmark run itself. Everything up to `/launch` is tested.
2. **The picker's fourth tile has not been looked at on screen** -- built
   and unit-tested only.
3. **No bundled `punktfunk-host` anywhere**, and see the KDE finding above
   for why the AppImage can't simply carry one.
4. `CurrentVideoCodec`/`SupportedVideoCodecs`/`Color444Status`/`HdrStatus`
   and the `ConfigStore` setters other than the output name remain
   unconfirmed/no-op.
5. Native `punktfunk/1` support is a distinct, later phase, paired with
   client-side work -- explicitly out of scope here, not forgotten.

## USB/input-driver angle (the other half of the original ask)

Punktfunk has **no real-device USB/IP bridging feature** analogous to
USBridge's own core product (sharing a physical USB device over the network).
What exists under "usb" in that repo is entirely **local virtual-HID / gamepad
injection**, same problem category as the already-parked ViGEmBus replacement:

- Windows: in-tree, self-signed **UMDF2 user-mode drivers** they own outright
  (`packaging/windows/drivers/`) -- `pf-xusb` (XInput-visible virtual Xbox 360
  pad, no ViGEmBus, no kernel bus driver -- the "HIDMaestro approach"),
  `pf-gamepad` (HID minidriver for DualSense/DS4/Edge/Steam Deck),
  `pf-mouse` (resident virtual HID mouse for a headless host), all sharing a
  host<->driver shared-memory protocol crate (`pf-driver-proto`) and a version
  gate (`MIN_DRIVER_PROTOCOL_VERSION`).
- Linux: `pf-inject` uses libei/KWin fake input/wlroots virtual pointer+
  keyboard for mouse/keyboard, and for gamepads either **uhid** (universal) or,
  specifically on SteamOS hosts, a **local USB gadget emulation**
  (`dummy_hcd` + `raw_gadget`, see `packaging/linux/steam-deck-gadget/`) so
  Steam Input promotes the pad through USB interface 2 the way it does for a
  real physical Deck. This is zero-network, same-machine kernel gadget
  emulation -- unrelated to USB/IP-over-the-wire device sharing despite the
  superficial "usbip" naming in that directory.

Relevant to the parked `SUNSHINE_WINDOWS_INPUT_TODO.md` task as *prior art* for
a from-scratch UMDF2 virtual-XInput driver (pf-xusb's README documents the
exact IOCTLs, wire formats and shared-memory layout, verified live against a
real RTX test box) -- worth reading before writing the C++/Rust equivalent for
Sunshine, regardless of whether Punktfunk itself ever becomes a backend.

## USB with Punktfunk or Sunshine as the streamer (2026-10-02)

USB works the same way under Punktfunk and Sunshine as under RustShine,
because none of it is the streamer's job. Both hosts are built from
`github.com/USBridge-Technologies/Streamers-Forks` (`punktfunk/`,
`sunshine/`), which carries the same change for each:

- **USB/IP passthrough** (any device, TCP) is the USB broker alone -- the
  agent starts it whichever backend is active.
- **A HID device over the stream** (a Wacom tablet sent with
  `LiSendRawHidEvent` on the ENet control channel, for input latency) needs
  the host to take that packet. The stock hosts drop it. The forks add
  `gamestream/usbridge.rs` (Punktfunk) and `src/usbridge.cpp` (Sunshine): when
  `USBRIDGE_USB_BROKER_CONTROL` is set (the agent sets it, see
  `streamhost/usb_broker_bridge.go`), the host forwards the packet body to
  the broker's `hid_stream` control command, and the broker rebuilds the
  device on a USB/IP port -- the same `RawHidHub` RustShine's streamer runs
  in-process. The host advertises `LI_FF_USBRIDGE_RAW_HID` when the broker
  has `hid_stream`. Raw HID devices are free except a Wacom tablet (VID
  `056a`), which the broker builds only with a Pro license.
- **Gamepads**: on Windows the patched host sends them to the same
  `hid_stream`, and the broker presents each as an Xbox 360 pad on usbip-win2
  (what RustShine does; the agent installs neither Punktfunk's `pf-xusb`
  drivers nor ViGEmBus for Sunshine). On Linux the host's own pads stay.
  `USBRIDGE_PAD_BRIDGE=1|0` forces either.

The agent tells the client raw HID is available (`RawHIDSupported`) only for
a host that answers `punktfunk-host usbridge-bridge` or
`sunshine --usbridge-bridge`, i.e. one built from the fork. The Sunshine the
agent bundles comes from the Streamers-Forks releases
(`scripts/fetch_sunshine.sh`; `v2026.1002.1.usbridge` is the first with the
change). Needs a broker with `hid_stream` (rust-shine 0.3.117 or later)
and, on Linux, the polkit rule that allows `usbip --tcp-port N attach` --
an older rule shows the USB permission as not granted until Grant is pressed
again.

Verified: unit tests on all three sides; live, a pad sent to a lab broker's
`hid_stream` is exported as `045e:028e` (`usbip list`). **Not yet run:** a
real client session against the patched host (tablet or pad), and anything on
Windows.

## Next step when resumed

Run the client's benchmark against an agent built from this branch with
Punktfunk picked, then work down "What's still open".

## Shared pairing with Sunshine/rust-shine (2026-10-04)

Punktfunk's own `cert.pem`/`key.pem`/`uniqueid`/`paired.json`/
`client-labels.json` (all under `PUNKTFUNK_CONFIG_DIR`, which is
`<stateDir>/punktfunk`) are now kept in sync with Sunshine's and
rust-shine's own trust stores, so a Moonlight client paired against one
backend doesn't need a fresh PIN after switching to Punktfunk. See
[`SHARED_PAIRING.md`](SHARED_PAIRING.md) for the full design; the
punktfunk-specific format details (confirmed from `gamestream/mod.rs`'s
`paired_path`/`load_paired`/`save_paired` and `gamestream/cert.rs`'s
`ServerIdentity::load_or_create`) live in
`agent/internal/streamhost/shared_auth_formats.go`. Not yet verified
against a *running* `punktfunk-host` process (unit-tested against the file
formats only) — see that doc's "What's confirmed vs. still open" section.

## PyroWave over the GameStream plane (2026-10-03)

The Punktfunk fork (Streamers-Forks `punktfunk/`) now offers PyroWave to a
USBridge client over GameStream, the same USBridge extension rust-shine
implements: `ServerCodecModeSupport` bit `0x01000000` (what
`punktfunkBackend.SupportedVideoCodecs` reads from `/serverinfo` and the
agent turns into a "pyrowave" codec mode) and the DESCRIBE line
`a=rtpmap:99 PYROWAVE/90000`; the client answers `bitStreamFormat` 3.

Found on the way: the fork's packetizer advanced `streamPacketIndex` on
parity shards. moonlight-common-c strips parity before its depacketizer and
treats a gap between FEC blocks as a corrupt frame, so every frame bigger
than one FEC block (each PyroWave frame above ~40 Mb/s at packet size 1024,
and large H.26x IDRs) was dropped. Fixed in the same fork commit; verified
live at 150 and 300 Mb/s with no corrupt frames.
