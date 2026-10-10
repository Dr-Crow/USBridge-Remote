# Experimental independently authored source-component package

This package contains the USBridge agent plus separately hash-pinned source
streamer and source broker executables. These components are independently
authored protocol reconstructions with licensed public dependencies. They are
not recovered proprietary code and do not establish stock product parity.
The corresponding source snapshots, license notices and commit pins are
included in `source/`; binary/source hashes are in `BUILD-PROVENANCE.json`.

## Current acceptance boundary

- Linux X11 H.264 software capture through a trusted local FFmpeg executable,
  encrypted RTSP, ENet control and synthesized-silence Opus audio.
- USB/IP importer-client over mutual TLS 1.3 with CA/name/certificate-pin
  checks, explicit per-device consent, bounded transfers and cancellation.
- The USB client does not attach devices to an operating system. This is not
  the stock browser USBP/VHCI protocol.
- These entrypoints are explicit experiments. They are not enabled by normal
  agent startup and do not weaken the stock pairing, TLS or device permissions.

Use `components/MANIFEST.sha256` with the directory containing
`components/manifest.json`. The agent accepts `--source-streamer-mode` or
`--source-broker-mode`, plus `--source-component-directory`,
`--source-manifest-sha256` and a separate `--source-state-dir`. Each mode reads
its versioned private JSON protocol from stdin. Never put session keys or
private-key bytes in process arguments or logs. See the included
`SOURCE_STREAMER_PROTOCOL.md` and `SOURCE_BROKER_PROTOCOL.md` for the exact
fields, consent and shutdown requirements.

The streamer currently requires system FFmpeg with libx264/libopus and a
consented local X11 display. It synthesizes silence; native microphone/system
sound is not captured. Broker credentials and expected device must be supplied
by the operator; the package contains no credentials, pairing secrets or
preselected physical device.

CI acceptance runs against an isolated Xvfb and a synthetic loopback mTLS USB
service, not a user's desktop or physical USB hardware. Inspect the pipeline's
media/lifecycle and broker receipts for the exact tested commit. The separate
LAN Docker test verifies its host/test-client internal network; it does not by
itself demonstrate zero WAN egress for this entire source-component package.
Windows/macOS source runtime, physical device compatibility, native input,
hardware acceleration and full desktop interoperability require additional
acceptance. A successful source process launch alone does not establish them.
