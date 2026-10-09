# Upstream integration checkpoint: October 9, 2026

## Scope and provenance

Integration branch: `integrate-upstream-2026-10-09`. The working
`agent-capability-policy` branch remains unchanged during validation.

- Fork parent: `908be49675f1226bb38905c385c350046deb054f`.
- Upstream parent: `b78722269924eceec23d575445926bf33879b1b9`.
- Merge base: `6f9d27223b310e8c6726b458739c8ae86a5b52dd`.
- Eight upstream commits affect 46 text files and the Moonlight gitlink.
- Every fetched text version was checked against its Git blob SHA before
  three-way integration, preserving files without trailing newlines.

## Changes taken from upstream

- Capture-resolution-specific YUYV preference.
- Linux X11 DPI stabilization and accompanying client configuration/UI.
- Raw USB peripheral selection and revised tablet-only paid-status display.
- Linux microphone and MIDI uplink support, with Moonlight submodule
  `477d2bec5d79be165b114397b064afe5ca1acfd0`.
- Default microphone/MIDI USB sinks when the operator has not set a value.
- Staged RustShine selection without a valid entitlement, and preserving
  the selected/running backend when a cached entitlement expires.
- Updated upstream documentation/translations.

Upstream now describes ordinary USB devices and tokenless/offline basic
streaming as free, retaining paid pen-tablet and 4:4:4 features. This is a
source-policy statement, not verification of a newly downloaded stock
streamer or broker. Older pinned component behavior may differ.

## Conflict resolutions and fork invariants

Three text files conflicted. The startup path retains the fork's staged-file
helper (including its local runtime path) while adopting upstream's
token-independent backend selection. Expiry adopts `dropEntitlementToken`
without switching backends. The refreshed-token signature/hardware
verification added by the fork remains intact. Its regression test now
checks both saved backend intent and live backend preservation.

USB consent copy remains component-neutral and explicitly states enabling
the broker does not itself share devices. Existing strict network policy,
local-runtime defaults, provisioning consent, pairing/authentication/TLS,
and pinned component verification are preserved. No component hash or
experimental binary patch is updated by this integration.

CircleCI branch filters include this integration branch for all six existing
jobs. The known-good public-client oracle used by the separate reconstruction
repositories is not changed by the agent's submodule update.

## Validation at publication

- All 138 fetched base/ours/upstream text versions match their Git blob SHA.
- All 34 changed/new Go source files parse and are gofmt-clean.
- No merge-conflict markers remain.
- Focused USB license-display tests passed locally with race detection and
  ten repetitions. This tests display classification, not device passthrough.
- The focused Linux MIDI source/test selection compiled with race detection;
  its live capture test was skipped because no test MIDI device was selected.
  This is compile evidence only, not a MIDI capture acceptance pass.
- Full agent tests, platform builds, browser smoke tests and Docker tests
  have not run locally: this is a partial source checkout. CircleCI results
  for this exact integration commit must be reviewed before adoption.
- No microphone, MIDI, physical USB, GPU capture or remote streaming
  acceptance result is claimed for the upstream additions.

## Fresh stock components

The newest public agent release observed during this review remains
`release-v3.0.104` (October 5). The proprietary component resolver remains an
authenticated vendor endpoint; source changes on main do not establish its
current component version. No newly issued credentials, fabricated tokens,
or replacement component pins were used in this review. Existing v0.3.131
archives remain the rollback/reference baseline. A fresh authorized
component manifest/archive must be obtained and verified before reporting
new stock-runtime behavior or updating supported-version assumptions.

## Follow-through

1. Review the exact-commit six-job CI matrix, fix integration regressions,
   and publish verified artifacts without changing the working branch.
2. Compare authorized fresh signed component manifests and archives with
   the preserved baseline, recording versions, hashes and CLI differences.
3. Test tokenless basic startup and new microphone/MIDI behavior separately
   from source comments. Preserve driver/device consent and isolate hardware
   tests from normal workstations.
4. Reconcile the independent broker/streamer reconstruction roadmap with
   verified upstream protocol changes; do not reset the existing research.
