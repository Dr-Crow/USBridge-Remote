# Secure WebTransport v1: static foundation

This is an independently authored browser-side contract and safety layer,
using the public upstream client's media layout as comparison evidence:
[upstream 3.0.114 player](https://github.com/USBridge-Technologies/USBridge-Remote/blob/8524b84bb06f17a97dcee96f8a2b4ac798cea310/client/internal/webrtcweb/wt_player.js).
It is deliberately versioned `usbridge-webtransport-v1`; it does not claim wire
compatibility with an unversioned stock endpoint.

## Current implementation and remaining gate

The page loads static same-origin player, worker, policy, lifetime and wire
modules before starting Go. Every asset is required and hash-covered by the web
package and LAN host. There is no `Function`, `eval`, generated worker source or
new CSP exception. The worker probe uses no decoder, network, device or capture
API. The frozen browser API explicitly advertises `transportEnabled:false` and
`supported():false`. Existing WebRTC selection is unchanged.

The implemented foundation includes bounded control/media parsers, exact
capability/session validation, browser-trusted TLS destination policy, a single
absolute setup deadline, abort propagation, bounded reader cleanup and explicit
cleanup uncertainty. Actual QUIC connection, server grant issuance, decoding,
playback and GUI selection remain disabled until the private server and real
browser acceptance are integrated. A static-worker pass is not a streaming pass.

## Agreed first server/client profile

- HTTPS HTTP/3 extended CONNECT at exactly `/webtransport`; no query credentials,
  redirects, userinfo, fragments, certificate-hash override or browser warning
  bypass. The endpoint must equal an independently authorized paired destination.
  Strict LAN additionally requires a literal local/private IP.
- The server checks the exact configured browser Origin before accepting.
- A fresh 32-byte random ticket is supplied by the authenticated agent, bound to
  session, origin, media configuration and expiry, and atomically consumed once.
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
`input` is false and `channels` is empty. A later session owner must additionally
match dimensions/rate to the authorized grant before starting decoders.
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
