# Local runtime research mode

## What was established

The v0.3.131 USB broker recognizes `--allow-unlicensed` but explicitly rejects it
in its entitlement-enabled release. This was reproduced on the Linux binary in
both agent and loopback roles. RustShine refuses startup without a valid token.

Ghidra recovered the broker's rejection branch and its separate token verifier.
The Windows verifier decodes an embedded public key and describes it as a
build-time constant. It is not an environment-variable override. Linux and Windows
broker pseudocode also show independent USB-class/tablet checks downstream.
For this exact Windows broker image, the rejection function is at
`0x14001e950` and the verifier at `0x140078cf0`; Linux uses Ghidra image base
`0x100000`, with the rejection function at `0x2d6c00`. These are analysis
addresses for pinned builds, not portable patch offsets.
Decompiler output has inferred types and warnings; it is not recovered original
Rust source and is not a proof that every execution path has been enumerated.

An offline experiment used a fresh, in-memory local signing key. Both unmodified
Linux components rejected its synthetic token. Copies in which only the embedded
verification key was replaced accepted it and progressed to their listeners.
A subsequent local control-handshake test reported `wacom: false` with local
Free claims and `wacom: true` with local Pro claims. No USB descriptor or input
was sent and no tablet was attached. This verifies the entitlement capability
decision, not actual pen input.
The temporary copies/token were removed. No synthetic token was submitted to a
vendor server and no vendor private key was recovered or forged.

**This is a modified-runtime approach, not a working debug flag and not execution
of byte-identical stock binaries.** The public agent can automate it without
maintaining Rust source, but its runtime copies are modified binaries. Startup
acceptance does not demonstrate successful hardware streaming, 4:4:4 encoding,
USB attachment or tablet input.

## Opt-in implementation

Ordinary double-click launches can use **Gear → General Settings → Experimental
local runtime**. Enabling it requires explicit confirmation and saves
`local_runtime_enabled` in the engine's configuration. It does not consent to
component downloads or USB sharing. The default remains off.

After saving, quit the engine and double-click the agent again. Closing a window
only hides it; a thin GUI attached to a headless engine does not stop that engine.
Stop an existing headless engine separately before relaunching. Changing the
checkbox never changes the running engine's mode or interrupts a stream.

`--local-runtime` and `USBRIDGE_LOCAL_RUNTIME=1` remain one-launch overrides.
Remove these overrides as well as turning off the saved preference to return to
vendor mode. Service installation is rejected for either saved or overridden
local mode.

The streamer badge reads **Pending** for a saved preference on a
vendor engine, **Enabled** before local preparation, and **Patched**
only after successful hash-checked preparation in this engine session. An empty
badge has no background. Disabling a saved preference leaves the active engine
and its prepared badge unchanged until restart. General Settings offers a manual
refresh of safe setup diagnostics without tokens, account links or raw logs.
Prepared status does not imply active video streaming or tablet input.

1. Select USBridge streamer and explicitly approve its component setup. Missing
   binaries still download through the genuine free-entitlement flow. USB retains
   its separate consent. No device is automatically shared.
2. The research mode accepts only the six audited v0.3.131 executable hashes and
   checks the single expected embedded key constant before replacement. Unknown
   releases fail closed instead of guessing offsets or silently patching them.
3. Each agent process generates one fresh local Ed25519 keypair in memory and
   creates a private per-session directory beneath `StateDir/local-runtime`.
   Downloaded originals, archive hashes and vendor manifests are unchanged.
4. The agent prepares runtime copies with the local public key and separate
   locally signed Pro test claims. The private key is not stored or uploaded.
   Claims expire after 24 hours and are renewed hourly while the agent runs.
5. The original vendor token remains separate in normal configuration. Local test
   claims are never used for component downloads, billing, TURN or signaling
   services. This does not unlock vendor-hosted paid services.
6. Copies are integrity checked before reuse. Windows copies only the pinned
   codec DLL alongside the streamer, never credentials/configuration. Mac copies
   are ad-hoc signed; notarization and stable privacy permissions are not supplied.
7. Normal process exit attempts to remove its own session directory and clears
   its in-memory private key. A crash can leave copies and expiring local tokens
   under `local-runtime`; they can be removed after all test processes stop.

## Security and operational boundaries

- Pairing, transport authentication, TLS, USB consent and protected-device rules
  are not removed. A different entitlement tier is not permission to bypass them.
- The signed privileged Linux launcher remains unchanged. Research mode refuses
  to use it; it does not grant CAP_SYS_ADMIN, install a service or alter OS security
  settings. Linux KMS capture may therefore be unavailable in this mode.
- `--local-runtime --install-service` is rejected. This is an interactive research
  build, not an elevated unattended deployment.
- Mac runtime-copy signing changes executable identity; capture/accessibility
  permissions may need user handling. No Gatekeeper/quarantine override is applied.
- Component auto-update checks are suspended in this mode because its executable
  hashes are deliberately pinned. Turn the mode off for ordinary vendor updates;
  a new component version needs a new audit before local-copy support.
- This does not make the whole agent network-independent: Tailscale, certificate
  registration, pairing/network services and optional account actions retain their
  existing behavior. See NETWORK_AUDIT.md.
- There is no macOS Intel vendor component pair in the inspected manifest. The
  Intel agent build is useful with its compatible open-source backend, not these
  ARM64 Rust runtime copies.

## Validation

Settings tests cover saved opt-in, unchanged live mode and component consent,
failed persistence, thin-client API handling, service-mode rejection, pending and
prepared badges, and hiding empty badge backgrounds.

Unit tests cover default-off behavior, rejecting unknown inputs/ambiguous key
constants, preserving source bytes, signature separation, hardware-bound local
claims, expiry and renewal. An explicit integration test starts only the hash-pinned
broker with loopback-only ephemeral listeners, checks its `raw_hid` and `wacom` Pro capability response, preserves the
original hash, and detects subsequent copy tampering. It never attaches a device.
The Linux integration test passed during implementation. Windows CI runs the same
probe after downloading verified components. Actual device/video acceptance and
Mac runtime-copy execution are separate outstanding tests; consult the final build
report for which CI results actually passed.
