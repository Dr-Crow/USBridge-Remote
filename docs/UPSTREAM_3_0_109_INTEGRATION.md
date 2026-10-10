# Upstream 3.0.109 integration

Integration prepared October 10, 2026 against fork `d49d0e454b4b39dc93b1fb72df4a8c475fdf00ad` and upstream `a75c7a5d4d6e178b683aa4fb595bfec947b41c27`. The merge base is `e509a87a1096e6b1ce275ed06d277103f5d93257`.

## Included changes

- Bounded app and streamer log rotation.
- Desktop USB KVM initial setup flow; browser clients hide this option.
- Custom agent API host/port handling and matching Moonlight ports.
- Linux/macOS host-load benchmark readers.
- Upstream port and benchmark documentation.

All 31 changed upstream files were verified against their Git blob hashes. Two import-only conflicts in agent main/app were resolved by preserving both fork imports and upstream logcap imports. Existing local-runtime and network-policy changes remain in place.

## Validation before publication

- New agent logcap, netutil and hostload package tests passed.
- Ten focused agent packages/commands passed with the race detector, including app, UI, local runtime/components, network policy, remote lock and streamer launcher.
- Agent `go test -race -tags ci -skip '^TestCheckRootOwned_SystemPaths$' ./...` passed; `go vet -tags ci ./...` passed.
- The skipped ownership test fails on this cloud executor because `/usr/bin` is not root-owned. This is an explicit local validation exclusion, not an application code change. CI must validate the supported host environment.
- Initial local attempts also encountered missing X11 headers/linker setup and VCS stamping in the materialized checkout. Local toolchain configuration corrected those before the final agent checks.
- Client logcap/models tests passed. Full native client validation is deferred to a complete checkout/CI; the initial partial local checkout lacked required packages. No complete client suite pass is claimed here.

## Remaining acceptance

The six-job CircleCI workflow must run on this merge. Real Windows/macOS GUI behavior, USB bootstrap hardware, custom-port connectivity, and physical audio/video/USB sessions have not been validated by these unit tests. The source-built research streamer and broker remain incomplete replacements. This merge does not establish feature parity or fully offline end-to-end operation.
