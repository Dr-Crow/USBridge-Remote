# Experimental independently authored source-component package

This package contains the USBridge agent plus separately hash-pinned source
streamer and source broker executables. These components are independently
authored protocol reconstructions with licensed public dependencies. They are
not recovered proprietary code and do not establish stock product parity.
The corresponding source snapshots, license notices and commit pins are
included in `source/`; binary/source hashes are in `BUILD-PROVENANCE.json`.

This is a developer experiment, not a self-contained one-click application.
The source path currently runs on the same host through a supplied-session
JSON interface. It does not yet provide source-only GUI setup, new-client
Moonlight HTTP pairing, or authenticated remote media forwarding. The stock
GUI path continues to use its existing components.

## Current acceptance boundary

- Linux X11 H.264 software capture through a trusted local FFmpeg executable,
  encrypted RTSP, ENet control and synthesized-silence Opus audio. Optional
  X11 keyboard/mouse input requires separate consent and advertised support.
- Native Windows GDI capture is compiled in the Windows source build. Its
  combined package acceptance exercises real supervisor startup, encrypted
  OPTIONS/TEARDOWN, EOF/restart and mTLS USB fixtures without starting capture.
  Windows desktop media and input are not established by that no-capture gate.
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

The streamer requires system FFmpeg with libx264/libopus and explicit capture
selection: a local X11 display on Linux, or literal `desktop` on Windows.
Windows uses a bounded width/height region at the primary-display origin; it
does not scale the entire desktop and does not support input injection. Use
an absolute local-drive FFmpeg `.exe` path on Windows. It synthesizes silence; native microphone/system
sound is not captured. Broker credentials and expected device must be supplied
by the operator; the package contains no credentials, pairing secrets or
preselected physical device.
FFmpeg, Linux X11 and their system libraries are not bundled in the archive. CI
provisions them separately and supplies a trusted absolute FFmpeg path. Use
only a display you explicitly consent to capture; the automated acceptance
creates its own virtual display and does not select an existing user display.

CI acceptance runs against an isolated Xvfb and a synthetic loopback mTLS USB
service, not a user's desktop or physical USB hardware. Inspect the pipeline's
media/lifecycle and broker receipts for the exact tested commit. The
`internal-network/result.json` acceptance runs the actual packaged agent with
both source components inside a Docker internal network, using unchanged pinned
public-client code for video/audio and separately consented isolated-Xvfb input,
plus a mutual-TLS USB fixture. The test explicitly requires blocked WAN TCP.
This demonstrates operation with egress blocked, not that a process never
attempted a WAN request. A missing receipt is not a pass.
Windows desktop capture/media, macOS source runtime, physical device compatibility,
hardware acceleration and full desktop interoperability require additional
acceptance. A successful source process launch alone does not establish them.
