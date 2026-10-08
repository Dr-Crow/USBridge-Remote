# Automatic normal launch: progress and remaining acceptance

8 October 2026. Work continues directly in the cloud checkout.

## This change

Normal configuration loading enables the existing local runtime by default on
Windows x86_64, Linux x86_64 and macOS ARM64, the audited component pairs. A normal
interactive launch no longer needs the experimental toggle, a PowerShell flag or
an extra restart to enable that mode. Explicitly saved `false` is retained across
save/load; unsupported platforms keep their prior backend choices.

This does not select a streamer, consent to USB device sharing, install drivers,
change OS permissions, or silently add a public download fallback. Selecting a
streamer still uses the existing provisioning flow. Missing or unknown components
must produce their existing errors, not be marked prepared.

## Limits still open

- This is the default-mode slice, not complete first-launch acceptance.
- The existing guard rejects local runtime in a Windows system service and rejects
  Linux privileged KMS launch. That guard remains. With the new default, service
  users must explicitly disable local runtime to retain the stock service path.
  Full upstream-equivalent service setup is not yet delivered.
- macOS Intel does not have an audited matching Rust component pair.
- Provisioning choices and runtime outbound policy still need final integration.
- Physical capture/streaming/USB, component empty-ICE parser acceptance and process
  traffic observation with Internet blocked remain unverified.
- Config regression tests passed in the cloud. Broader tests and exact-commit CI
  must be checked separately before calling a downloadable build ready.
