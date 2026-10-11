# Signature-only process owner extraction

Source: public [Dr-Crow/USBridge-Remote at c451d77c6bd24028f7281e69ab8aa29c0ff2d1c8](https://github.com/Dr-Crow/USBridge-Remote/tree/c451d77c6bd24028f7281e69ab8aa29c0ff2d1c8/.circleci/source/windows-preview-acceptance).

The exact Git tree was read from GitHub. Every retained source snapshot was
checked against that commit's Git blob identity, rather than inferred from a
local checkout HEAD. `PROVENANCE.json` records the exact relevant Git blob and
SHA256 identities. `testdata/upstream` contains unmodified public-source blobs.
The included upstream GPLv3 license is also Git-blob verified.

## Original native evidence, not extraction execution

The original c451d77 signature-probe receipt reported the literal startup
handshake, suspended-root inventory 1/1/1, locked installed System32 PowerShell,
one identity-verified System32 conhost, total lifetime process count 2, both zero
exits, natural empty-Job retirement, and joined cleanup. The receipt's SHA256 is
recorded in `PROVENANCE.json`; it is not supplied as evidence of this package's
execution. Its OS binary pins are machine-specific continuity measurements and
are not hard-coded as universal trust roots.

This extracted package has only been exercised through portable tests, race
checks, vet, and Windows/amd64 cross-compilation. No native Windows extraction
run was available. The original receipt does not establish native proof for
this extraction, any changed deadline, any dependency loading, or any caller's
separately reviewed signature/publisher/timestamp policy.

## Extraction boundary

Retained from the exact source:

- `signature_owner.go`: exact two-member admission and late/unknown/identity/
  duplicate/forced-retirement negatives, plus startup marker and graph freeze.
- `signature_owner_windows.go`: installed System32 discovery, locked conhost
  identity, retained handles, suspended PowerShell verification, atomic startup
  inventory, lifetime accounting, readiness-before-input handshake and exact
  natural retirement proof.
- `winapi_windows.go`: only process, pipe, handle, Job Object and lock primitives.
  No viewer/source policy, window discovery, GDI, screenshot, desktop, rendering
  or general command interface is included.
- `retirement.go`: original bounded retirement inventory helper and negatives.
- The immutable original catalog-aware PowerShell query and its fixed five
  arguments. It remains exported as `SignatureScript`, with its original SHA256.

The source reproduction tests compare token streams of unchanged declarations
and explicitly transformed startup/collection declarations. They verify every
upstream blob and check for forbidden generic capability surface.

## Deliberate adaptations requiring native revalidation

1. Package `signatureowner` exposes one-shot `Run(context.Context, Spec, []byte)`.
   The old graphics-specific receipt and signature parser are not reused.
2. The caller must provide an immutable reviewed script hash; the script is
   locked and hashed, never supplied as runtime shell text. The original script
   is an example, not a general publisher authorization policy. A separately
   reviewed script may load up to eight caller-pinned in-process dependencies,
   each at most 64 MiB, held write/delete-denied through cleanup. A dependency
   pin may come from a validated source-build receipt. No untrusted input may
   select scripts, dependency code, executable paths or pins.
3. Fixed `-NoLogo -NoProfile -NonInteractive -File <script>` invocation and a
   minimal process-local environment are internal. The package has no arguments
   or environment override, command CLI or caller child allowlist. Scripts must
   not use Add-Type/compiler children, execute acquired images, or launch any
   process. Unknown/later/third members fail the same ownership policy; the
   package is an owner/validator, not a sandbox for hostile PowerShell programs.
4. Both installed System32 executable hashes are caller-supplied continuity
   pins, checked against locked files. Suspended root verification additionally
   compares the running image's file identity/hash with that held root file.
5. One absolute deadline is established at Run entry and used throughout all
   stages. Timeout defaults to 30 seconds and has a hard 30-minute maximum.
   A 900-second trusted batch uses one budget, never one renewed budget per
   queried file. Context cancellation shortens it. The independent watchdog
   closes the Job on cancellation/deadline, then joins. Startup is additionally
   capped at the original 5 seconds; input write at 3 seconds; incomplete-retired
   inventory at 1 second. Forced cleanup has one separate 3-second join budget.
6. At most two newline-terminated stdout lines are allowed (readiness and one
   JSON object); result/request are bounded to 4096 bytes and the script to
   65536 bytes. JSON must be valid UTF-8, have an object root, no duplicate/case-aliased keys,
   no trailing value and depth at most 32. Stderr must be empty. The caller still
   owns schema validation and signature/trust decisions.
7. The work directory is explicit, locked, and resolved-path checked. Script and
   pinned dependencies must be direct children. Caller owns staging and ACLs;
   the library changes no files, registry, certificates, execution policy or
   security settings. File locks are released only after cleanup attempts.
8. Job handle closure and native queries are serialized to prevent reuse races
   with the watchdog. Native launch failure joins are checked. Failure output is
   cleared and never returned. A successful result requires natural retirement,
   both zero exits, complete worker joins and watchdog join, with no safety kill.
   Input writers own a separate copy so failed joins cannot race caller cleanup.
9. Post-review resource accounting tracks every acquired file/pipe/process/Job
   close, including normal release after natural retirement. `ResourceCloseError`
   preserves a primary failure, reports fixed resource categories and clears
   output. `ResourcesReleased` cannot be true with an unjoined worker or failed
   close. Native fixtures retain caller-owned work directories on uncertainty;
   their unknown-child fixture uses explicit `UseShellExecute=false` and fixed
   harmless System32 arguments, not a shell broker.
10. The 4096-byte protocol cap is retained after explicit batch-caller review:
   the <=2048-entry inventory is a separate hash-locked <=4 MiB dependency file;
   request/result contain only schema/counters/hashes. Any larger bounded receipt
   stays in the owned directory for the caller's exact count/order/hash checks.
   Boundary tests reject oversized protocol data rather than truncating it.
11. The API supports only Windows/amd64, the verified source ABI. Other platforms
   return `ErrUnsupported` after portable input validation, without launching.

## Native prerequisite, still outstanding

On an authorized Windows/amd64 executor with Go 1.26.9:

    go test -count=1 -timeout=5m -tags signatureownernative ./...

The tagged tests exercise the original fixed-script query, wrong/missing
readiness, partial/extra output, stderr, nonzero exit, timeout, unknown descendants,
and prelaunch pin failures. They use only test-owned scripts and installed OS
binaries; they never run Add-Type or a compiler. Repeated native runs, cancellation
races, dependency-lock behavior, and the actual separately reviewed caller query
remain necessary before production integration is represented as validated.
