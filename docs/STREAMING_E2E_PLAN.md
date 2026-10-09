# Streaming end-to-end acceptance plan

Updated 2026-10-09 UTC. Status: scoped, not a passing streaming test.

## New acceptance evidence

The owner reported on 2026-10-09 UTC that the new Windows build installed and
launched seamlessly without PowerShell. The precise archive hash was not specified
in that report. Record this as normal-launch installation acceptance, not evidence
of video, USB, premium formats, or WAN-isolated operation.

Existing CI executes the packaged Windows, Linux x86_64 and macOS ARM64 agents
headlessly and verifies operator TLS. These probes do not exercise capture.

## Staged topology

1. **Capture prerequisite:** inventory real capture devices, encoder support,
   display/session availability and versions on the chosen executor. Fail or mark
   unavailable explicitly; never convert missing hardware into a passing stream.
2. **Single executor, separate host/client processes:** eliminate cross-runner
   discovery and credentials initially. Use a deterministic animated test pattern,
   normal pairing and verified TLS, then count decoded frames in a separate client.
   This is process-level integration, not proof of two-machine networking.
3. **Two concurrent executors:** add an ephemeral, narrowly scoped overlay when
   cross-OS or separate-machine behavior is needed. Launch both peers concurrently,
   exchange readiness/address information with a bounded rendezvous, and tear down
   on success, failure or cancellation. A serial `requires` dependency cannot keep
   a finished host job alive for the client. Workspaces carry files, not a live host.
4. **Offline acceptance:** provision all dependencies first, then run on an isolated
   local test network with only the declared peers reachable and capture attempted
   egress. Hosted overlay success alone does not establish offline operation.
5. **Hardware acceptance:** real capture/GPU encoders, audio, input and USB devices
   get separately labeled runs. Synthetic video cannot validate physical USB.

## Capture compatibility is the first gate

The inspected Linux v0.3.131 RustShine help lists V4L2 and DRM/KMS capture. It does
not advertise X11/Xvfb capture. Xvfb can exercise GUI clients or a compatible
Sunshine baseline, but by itself is not a capture source for this RustShine build.
Its help mentions VKMS virtual displays conditionally on a desktop build; a CLI
option is not proof the relevant feature exists in the supplied executable or
that the runner kernel exposes the device. V4L2 loopback or VKMS would need an
appropriate kernel/module and explicit approval before system-level changes.

A GPU present in `nvidia-smi` is not sufficient evidence of an active desktop,
DRM connector, NVENC availability, Windows interactive session or DXGI capture.
Preflight those independently. Windows GDI capture is advertised by the inspected
CLI, but still needs an actual desktop/session. Never bypass OS capture consent.

Source evidence: the private RustShine research repository contains
`analysis/cli/linux-v0.3.131-help.txt` with binary hash and capture provenance.
The public client already has `client/cmd/pyrowavesmoke`: it connects to an
already-paired GameStream host, checks negotiated codec and counts decoded frames.
It does not perform pairing automatically and is not itself a full visual oracle.

## Overlay boundaries

Tailscale ephemeral nodes are suitable for short-lived CI peers, with explicit
cleanup via logout and a failure-path timeout. Use a dedicated test identity and
only peer-to-peer test ports; no production LAN/subnet routes or exit nodes.
Credential provisioning and persistent access need their own approved setup.
Do not write auth keys, pairing secrets, certificates' private keys or signed URLs
to artifacts. No such credentials have been created for this proposal.

The current strict local URL policy accepts private/link-local/loopback literal
IPs. Tailscale's `100.64.0.0/10` addresses are not RFC1918 private addresses and are
not accepted by that policy. Do not broadly relax the production policy just to
make a test pass. A future overlay test needs a separately reviewed explicit peer
allowlist and certificate SANs, or an appropriate isolated private test network.

Hosted Tailscale coordination/relay traffic must be distinguished from application
traffic. A passing overlay stream does not demonstrate that USBridge has no vendor
callbacks, nor that the overlay works with no public Internet connection.

## Assertions and artifacts

- Exact agent/component hashes, platform, runner image, capture backend, codec,
  display mode and encoder; original vs reconstructed component clearly labeled.
- Normal pairing/authentication and verified certificate chain/hostname.
- Negotiated codec and format match the request; reject silent 4:4:4-to-4:2:0
  fallback when testing 4:4:4. A dropdown selection is not a codec assertion.
- Sustained decoded frames with a changing sequence marker/test pattern, not just
  a successful connection or a nonzero count of duplicate/black frames.
- Separately assert audio/input round trips where implemented; never extrapolate
  video success to USB or tablet behavior.
- Redacted logs, bounded timing/frame metrics, failure screenshot, egress evidence,
  and teardown report. Every unsupported/skipped stage remains explicit.

## Provider feasibility and costs

Official CircleCI documentation currently lists Linux and Windows GPU execution
and labels the GPU page for the Scale plan. Actual organization eligibility and
credit cost have not been verified. No GPU job has been enabled by this document.
Start with existing CPU runners where a valid capture source can be demonstrated;
confirm an explicit budget and access before adding a paid GPU resource class.
A dedicated self-hosted test machine is another option, but installing a persistent
runner or overlay on the owner's hardware requires separate approved setup.

Sources checked 2026-10-09 UTC:
- https://circleci.com/docs/guides/execution-managed/using-gpu/
- https://circleci.com/docs/guides/test/browser-testing/
- https://tailscale.com/docs/features/ephemeral-nodes

This plan adds no network permissions, account credentials, driver installation,
GPU spending or passing streaming claim. Reconstruction remains the primary work.
