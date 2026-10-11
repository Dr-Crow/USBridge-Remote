# Secure WebTransport v1: static foundation

This is an independently authored browser-side contract and safety layer,
using the public upstream client's media layout as comparison evidence:
[upstream 3.0.114 player](https://github.com/USBridge-Technologies/USBridge-Remote/blob/8524b84bb06f17a97dcee96f8a2b4ac798cea310/client/internal/webrtcweb/wt_player.js).
It is deliberately versioned `usbridge-webtransport-v1`; it does not claim wire
compatibility with an unversioned stock endpoint.

## Current implementation and remaining gate

The page loads static same-origin player, worker, policy, lifetime, session and wire
modules before starting Go. Every asset is required and hash-covered by the web
package and LAN host. There is no `Function`, `eval`, generated worker source or
new CSP exception. The worker probe uses no decoder, network, device or capture
API. The frozen browser API explicitly advertises `transportEnabled:false` and
`supported():false`. Existing WebRTC selection is unchanged.

The implemented foundation includes bounded control/media parsers, exact
capability/session validation, browser-trusted TLS destination policy, a single
absolute setup deadline, abort propagation, bounded reader cleanup and explicit
cleanup uncertainty. The control-only owner now opens the supplied transport, writes a canonical
HELLO and validates WELCOME against the authorized grant. Its module is not
exposed by the disabled page API. Actual QUIC interoperability, server grant issuance, decoding,
playback and GUI selection remain disabled until the private server and real
browser acceptance are integrated. A static-worker pass is not a streaming pass.

## Agreed first server/client profile

- HTTPS HTTP/3 extended CONNECT at exactly `/webtransport`; no query credentials,
  redirects, userinfo, fragments, certificate-hash override or browser warning
  bypass. The endpoint must equal an independently authorized paired destination.
  Strict LAN additionally requires a literal local/private IP.
- The server checks the exact configured browser Origin before accepting.
- A fresh 32-byte random ticket is supplied by the authenticated agent, bound to
  session, origin, media configuration and two explicit expiry times, and atomically consumed once.
  Source consumes this grant; it does not enroll an account or mint persistent
  credentials. No capture/media/channel starts before ticket validation.
- Five seconds maximum for pre-authentication. Client setup uses one five-second
  absolute deadline across all stages, without independent resets.
- First profile is all-IDR H.264, stereo 48 kHz Opus synthesized silence,
  view-only, with no named channels. It does not enable input, clipboard, USB,
  microphone or webcam capture. Later profiles require deliberate capability
  and consent implementation.

### Control messages

`[type:u8][payload_length:u32 big-endian][payload]`, payload at most 4096 bytes.
The parser permits bounded chunks (65536 bytes, at most 64 completed messages)
and rejects unknown types, oversized lengths and truncated EOF.

HELLO type 1 contains compact canonical UTF-8 JSON with exactly:
`protocol`, `session_id`, `ticket`. Session IDs are 1–64 ASCII alphanumeric,
underscore or hyphen. Tickets are unpadded canonical base64url (43 characters).
Duplicate fields, alternate case, noncanonical encodings and extra fields fail.

WELCOME type 1 contains exactly `protocol`, `session_id`, `codec`, `width`,
`height`, `fps`, `audio`, `input`, `channels`. Values must match the narrow profile;
`audio` has exactly `codec:"opus"`, `sample_rate:48000`, `channels:2`.
`input` is false and `channels` is empty. The control owner additionally matches dimensions/rate to the authorized grant
before returning readiness; no decoder starts in this slice.
Type 7 is terminal takeover; it must never initiate automatic reconnection.

Discovery has exactly `protocol`, `ready`, `endpoint`, `tls_mode:"trusted"`,
`codec:"h264"`, `audio:"opus"`, `input:false`, `channels:[]`. Only explicit
capability absence (404/501) or a valid not-ready capability can allow the caller
to retain its existing WebRTC option, before any authorized stream starts.
Authentication/trust/policy/protocol errors and failures after HELLO are terminal.
The public agent does not yet provide this discovery/session issuance route.

### Media

Video uses one server unidirectional stream per access unit:
`[kind=1:u8][flags=1:u8][sequence:u32 BE][timestamp_us:u64 BE][H264 payload]`.
The new explicit sequence corrects the legacy player's arrival-order assumption.
Payload is 1–1048576 bytes. Sequences start at 1 and never wrap during a launch
(maximum 300 seconds). First-profile frames are independently decodable IDRs:
a completed newer frame can supersede a missing frame; late/duplicate frames are
dropped and increasing sequences with decreasing timestamps fail. The eventual
reader/decoder owner must enforce four active/completed frames and bounded writes.

Opus datagrams are `[kind=2:u8][timestamp_us:u64 BE][packet]`, payload 1–1275
bytes and subject to the actual negotiated QUIC datagram limit. Timestamps must
fit JavaScript's exact safe-integer range. Jitter/loss handling and A/V sync are
still required before an audio playback claim.

## Tests and proof boundary

Node tests cover malformed/canonical JSON, every split of a welcome frame,
10,003 oversized control lengths, chunk floods, timestamps, sequence ordering,
unsupported capabilities, exact destinations, pre-auth-only fallback, absolute
setup deadlines, cancellation, lock release and uncertain cleanup. Browser CI
loads the static worker under the unchanged production CSP. Asset omission,
symlink and package-hash checks remain mandatory. No latency/CPU advantage,
hardware decoder, real QUIC interoperability or user-ready transport is claimed.

## Control ownership increment

`session.mjs` consumes an already-authorized grant; it does not discover a new
peer, issue credentials, request devices or enable GUI selection. One shared
setup `Lifetime` covers readiness, stream acquisition, HELLO write and WELCOME.
A caller composing worker/codec initialization must pass that same lifetime and
close the returned owner if a subsequent stage fails. Failed setup cleanup uses
the original remaining deadline. Once established, shutdown has one separate
bounded five-second retirement budget, never renewed per operation.

Both an explicit parent abort signal and the borrowed setup lifetime's original
parent remain active through session retirement. The setup timer itself ends at
readiness. Close, expiry, peer loss and terminal takeover all retire the same
owner without automatic reconnect. Its `closed` promise preserves typed cleanup
uncertainty, including a late stream that cannot be released. Unknown controls,
truncated EOF, malformed WELCOME and widened capabilities are terminal. Encoded
HELLO scratch is cleared after its write settles; a browser write that cannot be
joined is reported uncertain rather than mutating bytes still in use. JavaScript
strings owned by the caller cannot be guaranteed zeroized; tickets must never be
logged or persisted.

Node tests and the strict-CSP Chromium gate exercise genuine stream primitives
with a fake transport. They prove control ownership only. Actual HTTP/3/TLS,
server-side one-use redemption, media decoding, stream-credit limits and the
four-frame media owner remain required before enabling the transport.

## Disposable browser trust fixture

The approved CircleCI-only test helper creates an isolated temporary HOME,
Chromium profile and NSS database. Its P-256 CA and leaf expire after one hour;
the CA has a critical 127.0.0.1/32 IP name constraint, and the leaf has only the
127.0.0.1 SAN. The CA private key is never serialized. The leaf key, certificate,
profile and database are removed after the browser/server retirement checks,
including failure cleanup. None is cached or uploaded. Receipts contain only
public fingerprints, fixed outcomes and cleanup booleans.

The test compares an untrusted fresh browser home with the CA-bearing fresh
home against the same loopback TLS1.3 endpoint. Chromium sandbox and ordinary
certificate validation stay enabled; no certificate-error, public-key hash or
host trust override is used. This establishes the isolated TLS prerequisite,
not HTTP/3 or media behavior. The private integration owns its real server and
fetches this exact public helper separately.

Chromium documents its Linux NSS database selection, including the existing
`$HOME/.pki/nssdb` fallback, in its
[certificate management guide](https://chromium.googlesource.com/chromium/src.git/+/refs/heads/main/docs/linux/cert_management.md).
The fixture uses Playwright's documented
[persistent context and explicit environment API](https://playwright.dev/python/docs/api/class-browsertype#browser-type-launch-persistent-context).

The authorized client grant distinguishes `ticket_expires_at_ms` (at most30
seconds to redeem during setup) from `session_expires_at_ms` (at most300 seconds
for the entire authorized session, no earlier than ticket expiry). Both are
issuer-supplied. HELLO retains only protocol/session/ticket; the server binds the
two deadlines to that one-use ticket. Every setup I/O stage checks redemption
expiry. After WELCOME the owner preserves the session deadline, rather than
ending a valid session when the redemption window closes. Monotonic elapsed
limits prevent wall-clock rollback from extending either initial allowance.
The UI remains disabled while actual HTTP/3 interoperability is being proved.
