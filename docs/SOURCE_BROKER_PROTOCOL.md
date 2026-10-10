# Experimental source-broker importer-client boundary

The independent source broker speaks USB/IP over mutual TLS 1.3 with an explicit
server certificate pin. It is not the stock USBP/browser broker and does not
attach a device to the operating system. Readiness always reports
`role: "usbip-importer-client"` and `os_attached: false`.

## Agent invocation

```
usbridge_agent --source-broker-mode \
  --source-component-directory /absolute/components \
  --source-manifest-sha256 <trusted-manifest-sha256> \
  --source-state-dir /absolute/experimental-state
```

Use a `manifest.json` with schema 1 and a `source-broker` component, profile
`source-broker-v1`, the actual `GOOS/GOARCH` platform, and the `broker-session`
executable as a hash/size-verified entry. The agent uses `--session-stdin` to
launch it, never passes credentials in command-line arguments, and does not
route through the vendor entitlement or modified-binary path. The agent probes the verified executable with `--capabilities` and requires
`session_supported: true`, matching platform/profile and all three v1 transport
capabilities. Unsupported builds fail before credentials are passed. Linux has
real agent integration coverage. The pinned broker source also passed native
Windows standalone CLI/mTLS acceptance; the combined agent wrapper on Windows
and macOS still needs native runtime acceptance.

The first newline-delimited JSON object contains `version: 1`, literal private
or loopback `address` with port, absolute local `certificate_file`, `key_file`
and `ca_file`, expected `server_name`, nonzero 64-hex `server_pin`, explicit
`bus_id`, `consent: true`, and `capacity` between 1 and 16. Key material stays in
local files. The broker enforces the mutual-TLS identity, CA, name and pin; the
server separately authorizes the selected device and client identity.

Ready includes protocol version, role, OS-attachment status, capacity and the
selected device's bus ID, bus/number and vendor/product IDs. The agent rejects
a different bus ID, unsupported role or claimed OS attachment.

Subsequent input objects have `op`:
- `transfer`: unique ASCII `id`, `direction` (0 OUT or 1 IN), `endpoint` 0–15,
  `length` 0–16384, optional `flags`, eight setup bytes, base64 OUT `data`, and
  optional `timeout_ms` 1–10000 (0 selects the broker default).
- `cancel`: the existing transfer `id`. Cancellation waits for native completion
  before the source server acknowledges USB/IP UNLINK.
- `close`, or stdin EOF: cancel and join active transfers, close the session.

Output is bounded typed `complete`, `cancel_requested`, `error`, and `stopped`
events. Completion carries the request ID, signed USB status, actual length,
and base64 IN data, or a sanitized cancellation/deadline/transfer error. Messages
are limited to 64 KiB and transfers to 16 KiB. No arbitrary child stderr is
forwarded. A clean `stopped` event plus successful child exit is required.

## Validation and remaining limits

Process fixtures exercise consent/identity/bounds validation, wrong-device and
false-attachment rejection, start/transfer/close/restart, and startup timeout.
The real agent executable and real source broker also passed a synthetic
loopback mutual-TLS server test with import, transfer, cancel, subsequent
transfer and close. This validates the actual process boundary and wire client.
It does not establish physical USB compatibility, kernel/VHCI attachment,
non-Linux runtime behavior, stock browser protocol compatibility or USB product
parity. Source commit pins and CI receipts must be recorded independently for
distribution artifacts.

The process-mode initial JSON line must arrive within 10 seconds. SIGTERM or
interrupt cancels a blocked initial read on supported platforms; inherited-pipe
I/O is process-scoped, bounded, and never reused after timeout/cancellation.
