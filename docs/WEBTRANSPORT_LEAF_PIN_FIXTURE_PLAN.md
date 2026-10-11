# Explicit WebTransport leaf-pin fixture proposal

Status: approved disposable fixture preparation. The owner approved the exact
one-hour leaf-pin experiment on 2026-10-11. The public helper has portable tests;
real browser/QUIC trust, authentication and media receipts remain pending in the
private component integration. This is not a production trust-policy change.

## Observed browser and reason to investigate

Public job463 installed Chrome for Testing151.0.7922.34 (Playwright1.62.0,
chromium1234). Its exact source uses the ordinary certificate verifier and then
requires a known public root when no explicit certificate hash is supplied:

- https://github.com/chromium/chromium/blob/151.0.7922.34/net/quic/dedicated_web_transport_http3_client.cc#L95-L110
- https://github.com/chromium/chromium/blob/151.0.7922.34/net/quic/crypto/proof_verifier_chromium.cc#L427-L431

The locally trusted HTTPS page passing does not establish that separate QUIC
condition. The current real-browser failure has not yet been attributed to this
exact code path. Developer mode, ignore-certificate flags and force-QUIC
exceptions are outside this proposal.

## Authentication primitive

The standard serverCertificateHashes option verifies SHA256 of the full leaf
DER certificate, not its SPKI. It requires a dedicated connection, current
certificate validity and a validity period no longer than two weeks; P-256 is
an interoperable allowed key type and RSA is excluded. The existing generated
one-hour P-256 leaf is within those bounds. Reference:
https://www.w3.org/TR/webtransport/#dom-webtransportoptions-servercertificatehashes

The proposed fixture uses exactly one32-byte digest. No wildcard, alternative
historical leaf, pooling, discovery-learned identity or trust-on-first-use is
allowed. Failure never retries using a different authentication mode.

## Source-level fixture input and ownership contract

The private harness owns certificate generation and the server process. It
would compute the digest directly from the exact generated public DER leaf,
validate its lifetime/SAN/key and bind it to the reserved loopback endpoint
before supplying the browser configuration. A closed fixture-only configuration
must identify these exact fields:

- role: webtransport-pinned-leaf-test-v1
- protocol: usbridge-webtransport-v1
- endpoint: the exact HTTPS127.0.0.1 ephemeral port and /webtransport path
- origin: the independently trusted HTTPS fixture page origin
- leaf_sha256: exactly64 lowercase hex characters, decoded to exactly32bytes
- leaf_not_before_ms and leaf_not_after_ms: current, maximum one-hour interval
- session_id, ticket, ticket_expires_at_ms and session_expires_at_ms: the same
  one-use server-bound grant already used by the strict control fixture
- public_client_commit and private_fixture_sha256: exact provenance pins

No certificate/private key is accepted from discovery or a browser request.
The ticket stays in the bounded HELLO, never a URL, command line, receipt or
console. The leaf digest is public identity material, but receipts need only
its fingerprint and fixed outcomes. Certificate keys and browser profile still
expire/remove under the approved disposable ownership boundary.

The initial probe must be labelled fixture-pinned-leaf in its receipt. It must
not pass a hash-enabled native transport behind the public discovery claim
`tls_mode: trusted` and present that as proof of the unchanged production trust
policy. The current public owner/policy rejects certificate-hash overrides.
Any later composition needs a separately reviewed explicit trust-mode contract.
The UI remains disabled until authenticated grant issuance, origin/destination
binding and real media/cleanup are proved together.

## Required negative and positive matrix

All cases use the same reviewed sandboxed browser process/profile ownership.
Every attempt has one bounded whole setup/session lifetime; no stage resets.

1. Missing/malformed/duplicate/extra digest, wrong algorithm, invalid hex/length,
   widened endpoint/path/query/origin or invalid interval: reject before native
   WebTransport construction and before any HELLO.
2. Correct endpoint with a different otherwise-valid leaf, wrong digest or
   expired/not-yet-valid leaf: browser.ready must reject; server authenticated
   open/start counts remain zero. Record that failure as trust-stage rejection.
3. Correct leaf identity with wrong/replayed/expired ticket: ready and bounded
   HELLO completion must first be established, then authentication rejects;
   no media/named channels/source starts. A TLS failure cannot pass this case.
4. Correct identity and fresh ticket: matching WELCOME, then explicit client
   close; exact server open/start/close accounting, natural server/browser exit,
   empty owned process group, joined sockets/readers and removed profile/keys.
5. Generated-video follow-on: public bounded parser/decoder only after WELCOME,
   exact128x72baseline420 blue/orange pixels, sequence/timestamp checks, no
   audio/input/capture, followed by the same natural cleanup checks.
6. Trust failure, parent cancellation, setup expiry, malformed control, excess
   streams, decoder failure and close failure remain terminal. No fallback,
   reused grant, automatic reconnect or forced-cleanup success.

For production, any future digest must come through an already-authenticated
agent-issued grant, bound to exact endpoint/origin/expiry. An unauthenticated
capability response cannot authorize a pin. This proposed fixture cannot itself
establish that production issuance or private-network deployment is finished.


## Callable public fixture helper

`.circleci/webtransport_leaf_pin_fixture.mjs` exports
`makePinnedLeafTransport(config, {NativeTransport, now, pageOrigin,
expectedPublicClientCommit, expectedPrivateFixtureSHA256})` and returns
`{authentication: "fixture-pinned-leaf", Transport, grant}`. Configuration contains the
closed fields listed above, plus exact `width:128`, `height:72`, `fps:30`.
The returned constructor accepts only the same endpoint and
`{congestionControl:"low-latency"}`. It rechecks expiry, burns its one-use native
construction attempt, disables pooling and supplies exactly one full-DER SHA256
hash to the standard native API. A failure never retries or downgrades trust.
The existing profile receipt supplies `leaf_sha256`, `leaf_not_before_ms` and
`leaf_not_after_ms` from the certificate it generated, together with explicit
`full-der-sha256` and `P-256` labels.

The module lives outside `client/web`; it is not copied into WASM/Docker web
assets. Production discovery continues to require `tls_mode:trusted` and must
not be repurposed to label this test injection. Native control-negative fixtures
can construct `Transport` directly and use the existing bounded wire parser.
A private generated-video composition must keep the distinct authentication
label through any test-only owner adapter. No public UI capability is enabled.


The caller must supply independently expected full public commit and private
fixture executable hash; regex-shaped but unequal provenance is rejected. The
returned `grant` is an immutable copy of the exact validated grant. The private
positive control fixture imports `ownAuthenticatedControl` from the exact pinned
`/transport/session_owner.mjs` and passes `grant: prepared.grant`,
`authorizedEndpoint: prepared.grant.endpoint`, and `Transport: prepared.Transport`.
For video it uses `ownAuthenticatedVideo` with the same immutable grant and owned
canvas. It must not manufacture a trusted discovery response. The test-only
negative HELLO path can intentionally send a wrong ticket after successful native
ready, with the same expected native identity and separately classified outcome.

`session_owner.mjs` is a lifecycle primitive for already-authenticated callers;
it does not interpret discovery or choose a trust mode. The only production
callers are the two `session.mjs` wrappers, which retain trusted-only discovery.
Repository import/caller checks and full browser/Docker packaging gates cover
this boundary. The independent private harness must pin the public source files
and assert the receipt authentication label for every positive/negative case.
