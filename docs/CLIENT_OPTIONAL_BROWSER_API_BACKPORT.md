# Optional browser API compatibility backport

This increment selectively adopts the intent of upstream commit
[c68870c00bc3397809aa83d304a412b9c5fa82e3](https://github.com/USBridge-Technologies/USBridge-Remote/commit/c68870c00bc3397809aa83d304a412b9c5fa82e3)
on stable agent commit `1d47f176c2d33511f9b15e515bd4f7a1f23c4da7`.

The WebHID overlay and Gamepad polling/enumeration treat missing browser APIs
as unavailable rather than panicking. The small WASM-only browser boundary also
handles null/non-function values, browser-denied calls and throwing property
getters. Calls retain their original receiver. The visibility hook uses the same
boundary. No permission is requested, no device is opened, and no transport or
fallback is added.

The helper has Node-executed Go/WASM tests for absence, invalid shape, receiver
and argument preservation, exceptions and property getters. The browser smoke
adds fresh pages with WebHID absent and Gamepad absent/denied, under the existing
production CSP and strict loopback request policy. Existing bundle build, native
model/transport tests, WASM vet and internal-network LAN host tests remain gates.
These are API-availability tests, not hardware passthrough acceptance.

Client VERSION, index.html, runtime-policy.js, runtime-config.js, bootstrap.js,
WebRTC defaults and the LAN CSP are unchanged. WebTransport, account sync, USB
claiming, device clock/network settings and vendor-host redirects are deferred.
The branch runs the three affected web/LAN jobs; stable promotion still requires
review of exact hosted receipts. Native agent code is byte-identical to `1d47`.
