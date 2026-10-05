# Shared Moonlight pairing across Sunshine / rust-shine / punktfunk

**Status (2026-10-04): implemented, unit-tested (32 tests, `go test -race`
clean), not yet verified against a live running backend process in this
session.**

## Problem

Sunshine, rust-shine (`usbridge-streamer`), and punktfunk-host are three
independent processes. Each generates its own self-signed TLS server
identity and keeps its own list of trusted client certificates, in three
different on-disk formats. Moonlight pairing is mutual TLS bound to the
exact server certificate a client saw during its original pairing — so
switching which backend is active used to look, to an already-paired
Moonlight client, like talking to a different host entirely, forcing a
fresh PIN every time. Unpairing a client was also backend-local: removing
it from Sunshine didn't remove it from rust-shine or punktfunk, so switching
back silently un-revoked it. On top of that, Sunshine's own admin-API
unpair call was reported to sometimes not actually persist the removal.

## What changed

New files, all in `agent/internal/streamhost/`:

- **`shared_auth.go`** — the canonical TLS identity (RSA-2048 self-signed
  X.509, PKCS8 key — confirmed byte-compatible with what Sunshine's real
  `pkey.pem` on this machine, rust-shine's `crypto.rs`, and punktfunk's
  `gamestream/cert.rs` all already produce) plus the canonical,
  fingerprint-keyed trust store (`identity/trusted_clients.json`) with a
  tombstone list for explicit removals. Entry points:
  - `EnsureSharedIdentity` — generates fresh on a brand-new install, or
    **adopts** whichever backend already has a real identity (priority:
    Sunshine, then rust-shine, then punktfunk) on an existing install, so
    rolling this feature out never forces already-paired devices to
    re-pair.
  - `ReconcileSharedAuth(stateDir)` — called at the top of every backend's
    `Start()`. Passive, union-only (never removes anything): reads each
    backend's own native trust file, folds anything new into the canonical
    store, and pushes the canonical store back into all three. Idempotent —
    safe to call on every single Start(), not just the first one.
  - `SyncAfterPair(stateDir, activeBackend)` — called right after a
    successful `SubmitPIN`, propagates the newly paired client to the other
    two backends immediately. Also the *only* thing allowed to lift a
    tombstone (see below).
  - `RemoveTrustedClientEverywhere(stateDir, activeBackend, identifier)` —
    called after `UnpairClient`. Force-rewrites all three backends' native
    trust files to exclude the fingerprint (a strict overwrite, not a
    merge) and tombstones it in the canonical store. This is what fixes the
    reported "can't delete in Sunshine" symptom: removal no longer depends
    on Sunshine's admin API actually persisting anything — the file is
    rewritten directly, the same file Sunshine reads at its own startup.

- **`shared_auth_formats.go`** — per-backend native format read/write,
  confirmed against real sources, not guessed:
  - **Sunshine**: `sunshine_state.json`'s `root.named_devices` (confirmed
    from a real file captured live on a dev machine — see the golden
    fixture in `shared_auth_formats_test.go`). Read/written as generic
    `map[string]json.RawMessage` so any field this code doesn't know about
    round-trips untouched.
  - **rust-shine**: `trusted_clients.pem` — PEM certs preceded by a
    `# uniqueid:<id>` comment line, per
    `usbridge-streamer-proto::pairing::PairingManager`.
  - **punktfunk**: `paired.json` (a bare JSON array of byte-number arrays,
    `serde_json::to_vec(Vec<Vec<u8>>)` — **not** base64; Go's default
    `[]byte` JSON encoding had to be bypassed explicitly, see the comment
    in `readPunktfunkTrusted`/`writePunktfunkTrusted`) plus
    `client-labels.json` (fingerprint → operator label sidecar).

## Why a tombstone, and why it can only be lifted by `SyncAfterPair`

A client removed via `RemoveTrustedClientEverywhere` must never be
resurrected just because `ReconcileSharedAuth`'s passive union sees it
still sitting in some backend's file (e.g. a backend whose own admin-API
unpair didn't actually persist — the exact bug being fixed). So removal
also tombstones the fingerprint, and the passive reconcile path skips
anything tombstoned, forever.

But a tombstone that can *never* be lifted would also permanently block the
operator from genuinely re-pairing that same physical device later
(Moonlight clients reuse their own persisted keypair, so a real re-pair
produces the identical certificate — indistinguishable from stale leftover
data by content alone). The fix: only `SyncAfterPair`, called in direct
response to a `SubmitPIN` call that just actually succeeded, may clear a
tombstone. Passive reconciliation never does. See
`TestReconcileSharedAuth_NeverLiftsATombstoneOnItsOwn` and
`TestSyncAfterPair_ClearsTombstoneOnGenuineRePair` in
`shared_auth_sync_test.go` for the pinned behavior.

## Wiring

- `sunshineBackend.Start`, `rustshineBackend.Start`, `punktfunkBackend.Start`
  each call `ReconcileSharedAuth(b.stateDir)` early, before launching the
  child process.
- `app.go`'s `SubmitMoonlightPIN` calls `streamhost.SyncAfterPair` after a
  successful `SubmitPIN`.
- `app.go`'s `UnpairSunshineClient` calls `streamhost.RemoveTrustedClientEverywhere`
  after (regardless of whether) the live admin-API `UnpairClient` call
  succeeds — the force-rewrite is the authoritative removal.
- `streamhost.BackendKind(b Backend) string` (`backend.go`) identifies which
  concrete backend a `Backend` interface value is, for the two call sites
  above.

## What's confirmed vs. still open

Confirmed by reading the actual source of all three streaming hosts (two of
them — rust-shine and punktfunk — are repos on this same machine;
Sunshine's own format was confirmed against a real captured
`sunshine_state.json`, not the upstream source, since the bundled Sunshine
binary isn't vendored here) and by the unit tests in this package.

**Not yet confirmed live:**
- An actual pair/unpair cycle against a *running* Sunshine, rust-shine, or
  punktfunk process in this session — no backend was running when this was
  built. The file-format and propagation logic is tested; the real-world
  admin-API interaction (timing, CSRF, concurrent live process + file edit)
  is not.
- The original "Sunshine can't delete a pairing" bug's root cause was never
  isolated — `RemoveTrustedClientEverywhere`'s direct file rewrite is a
  reliable fix for the *symptom* (the file ends up correct regardless of
  what the HTTP call did), not a diagnosis of why the HTTP call itself was
  failing.
- Punktfunk's server-identity file (`cert.pem`/`key.pem` under its config
  dir) being overwritten by the shared identity has not been tested against
  a running `punktfunk-host` — only the file contents/format were verified.

If you touch this again: re-verify by actually running two of the three
backends back-to-back with a real Moonlight client paired, switching
between them, and confirming no re-pair prompt and a real unpair sticking
across a restart.
