# Integrated agent local validation — October 9, 2026

Code under test: `65a1df87aa2bcaf856818185a6353107d3686844` on the upstream integration branch. This is separate from the accepted working branch.

## Source and environment

559 selected text files were materialized and verified against their Git blob hashes, including the agent and required public-client packages. Additional model/signature fixtures and 12 embedded image/font assets were supplied; the assets matched the fork's declared blob hashes. The public upstream supplied identical assets. No placeholder assets were substituted.

Go 1.26.6, Linux amd64, race detector. Missing X11 headers were supplied in a workspace-local prefix from official Debian packages (`libx11-dev` 1.8.12-1 and `x11proto-dev` 2024.1-1), using the already installed libX11.so.6. No system ownership, trust or security settings were changed.

## Results

- `go test -race -tags ci ./internal/...`: **failed overall**. 26 packages passed. The sole failing package in this final run was `internal/streamerlaunch`.
- Its failing assertion was `TestCheckRootOwned_SystemPaths`: this executor's `/usr/bin` is not owned by root. The privileged-launcher policy was not relaxed, the assertion was not skipped and the result is not reported as a pass.
- `go vet -tags ci ./internal/...`: passed.
- App and UI race tests passed using Fyne's `ci` software driver. This is not native desktop rendering, browser or hardware acceptance.
- Configuration, entitlement, streamhost, local components/runtime, network policy, update, TLS/certificates, USB service and the other reported packages passed.
- Earlier materialization attempts failed because model files, embedded assets and X11 headers were missing. Those input/dependency gaps were repaired before the final aggregate above; they were not worked around with altered source or fake resources.

## Still required

The exact integrated hosted build matrix, native GUI and OS permission flows, fresh proprietary component compatibility, real capture/camera/MIDI/isochronous USB devices, complete paired streaming, and blocked-WAN end-to-end tests remain open. In particular, the original v0.3.131 component baseline is not proven to implement the newest camera/ISO source features. Do not promote this checkpoint solely because the source-level tests passed in most packages.
