# Validation checkpoint, 8 October 2026

## Confirmed

- Agent automatic-start checkpoint cb1bb2d passed Windows job 68, Linux 71,
  macOS 69, web 70 and LAN-host 67. Windows completion was independently read
  from CircleCI; GitHub's pending status briefly lagged the actual result.
- Subsequent runtime/provisioning separation passed focused app, policy,
  entitlement, configuration, streamhost, Tailscale, account and certificate
  tests with the race detector. UI/update/benchmark tests and affected vet
  checks also passed. Exact-head hosted CI remains a separate check.
- Original Linux v0.3.131 archives were fetched via genuine provisioning and
  verified against the signed manifest and pinned executable hashes.
- The pinned stock Linux streamer accepted `--webrtc-ice-servers=` in its
  one-shot credentials-writing mode. This is parser acceptance, not streaming.
- The Linux modified-copy broker probe reported raw_hid and wacom capabilities;
  originals were unchanged. No physical device or input was sent.
- Web artifact cb1bb2d passed ZIP CRC and all 19 asset hashes. Its strict runtime
  configuration is supplied by the LAN host, not enabled in the plain static ZIP.

## Explicitly not established

The cloud full-package test attempt did not pass overall: X11 headers were absent
for remotelock and a launcher ownership assertion found `/usr/bin` not root-owned
in the cloud filesystem. Affected focused tests passed; these environment limits
were not hidden with new test exclusions. Hosted Linux is the full build gate.

Network syscall tracing was attempted for the broker test, but this cloud runtime
rejects ptrace, including one approved escalation attempt. No successful trace or
zero-egress claim is made. System security settings were not changed.

A real Chromium startup smoke was attempted in the cloud with its sandbox kept
enabled. Chromium could not create its process-singleton socket in this runtime.
No screenshot or real-browser success resulted. The CircleCI browser-lan job now
runs that acceptance on an Ubuntu machine using the source-built web workspace.
It checks startup/reload, viewport resize, local-only page requests and rejection
of public fetch/WebSocket/ICE configuration, and saves screenshots and a report.
Until that job passes and the screenshots are inspected, browser acceptance is
pending. The fixture uses loopback HTTP, not a bypassed certificate warning.

Physical streaming/USB/tablet, TLS trust onboarding, prolonged expiry/restarts,
all-platform subprocess traffic, and full offline end-to-end behavior remain
open. The Windows service and Linux privileged KMS local-runtime guards remain.
