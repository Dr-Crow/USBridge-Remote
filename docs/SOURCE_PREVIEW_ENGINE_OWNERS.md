# Owned-engine integration into the preview branch

This branch applies the public bridge and HTTP lifetime changes already tested
at `515d1bc718ae6f15f2b92171f225747bd81f5b47` to the exact preview baseline
`6bea3ef055d7b42bd33245334e4c7926cae39ece`. It does not enable the Windows
settings action or change component snapshots, capture/input consent, transport,
TLS binding, certificate issuance, or runtime provisioning.

The applied range is the agent-authored ownership delta after stable
`1d47f176c2d33511f9b15e515bd4f7a1f23c4da7`. The source files applied without
conflicts. The preview baseline already contained its own account/local policy
and Source preview paths; those paths remain in place. This is a focused
backport, not a claim that every stable/upstream file has been merged.

The constructor no longer opens the optional browser USB bridge. Run owns it,
and startup rollback closes and joins it. HTTP, HTTPS and the tailscale HTTP
listener have registered generations, bounded shutdown and Serve/request joins.
Hot replacement cannot acquire a new listener after the final registration seal.
Observed hijacks remain an explicit incomplete-worker condition. Route-specific
WebSocket ownership, clipboard callbacks and other background services are not
claimed fully retired by this increment.

## Required checks

- Loopback bridge and HTTP lifetime regression tests execute natively on Windows
  and under the race detector on Linux, including the full App compilation.
- The existing Linux native renderer, settings dialog and actual CLI/App.New
  disposable-container lifecycles remain mandatory.
- The existing Windows process, signed-module, exact-title window, generated
  changing-pixel and natural cleanup gates remain mandatory.
- The workflow contract checks both preview branch names select exactly these
  three jobs and publish only their source-free receipt directories. No source
  archive, executable or private component is added to artifact publication.

The known-good 6bea Windows receipt proves three CLI-generated media lifecycles,
39 owned-window samples and 393 source frames. It does not prove Windows
Manager composition or the real settings/App.New route. VM332 Manager execution
is separately blocked pending independent verification of the updated Windows
GDI+ signer. Earlier VM330 and VM331 are retired and must not be reused.

Hosted results for this merge must be recorded separately from those baselines.
Cross-compilation and local loopback tests are not native GUI acceptance.
