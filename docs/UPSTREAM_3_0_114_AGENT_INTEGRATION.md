# Agent-only upstream 3.0.114 integration

## Exact scope

Candidate is based on the tested fork `3937f2696efee30ae061ccd22523352ca63ff1b3`.
It backports the `agent/` delta from upstream
`a75c7a5d4d6e178b683aa4fb595bfec947b41c27` (3.0.109) to
`8524b84bb06f17a97dcee96f8a2b4ac798cea310` (release-v3.0.114).
Upstream comparison contains 26 commits and 121 changed paths; 21 are agent paths.

Sources:
- https://github.com/USBridge-Technologies/USBridge-Remote/releases/tag/release-v3.0.114
- https://github.com/USBridge-Technologies/USBridge-Remote/compare/a75c7a5d4d6e178b683aa4fb595bfec947b41c27...8524b84bb06f17a97dcee96f8a2b4ac798cea310

This is an agent-only backport, not a complete upstream merge. The client remains
byte-for-byte at the fork's tested 3.0.109 baseline. No upstream merge parent is
recorded, so deferred client changes are not falsely marked incorporated.
Experimental source-preview work stays on its separate branch.

## Included changes

- Demand-driven vendor device certificates, with account-token authentication and
  signed-out status guidance. Existing offline/operator trust paths stay local.
- UI-scale presets, Linux DPI initialization and reorganized cosmetic theme UI.
- Protocol picker presentation and upstream release-note/localization updates.
- Same-version GLFW dependency becomes direct; pinned build toolchain remains
  Go 1.26.9 with unchanged archive hashes and native test exclusions.

The update does not create an account or acquire account credentials. A signed-out
user cannot request a vendor certificate or generate its persistent device key.
RuntimeLocal and StrictLAN reject vendor requests before network construction.
Operator certificates take precedence before matching-SNI demand is recorded.
Cosmetic Pro theme choices remain independent of account/license state.
Stock component version/hash pins and all device-consent checks are unchanged.

## Deferred client/browser changes

Upstream adds a Chromium WebTransport default without a fallback compatible with
our pinned runtime, and evaluates its player using JavaScript Function. Our
strict local Content Security Policy does not allow unsafe-eval. The runtime
request guard would also need an explicit WebTransport boundary. These changes
need a separate compatible runtime/browser acceptance increment. No CSP weakening,
public-host redirect, client asset change or transport-default change is included.

## Integration corrections

Three import conflicts were resolved in favor of operator-first TLS, local runtime
controls/consent, and account-independent cosmetic themes.

Review/tests exposed three upstream lifecycle issues:
1. Pending issuance backdates the heartbeat timestamp. Using that timestamp for
   demand retry allowed immediate repeated SNI retries. Demand throttling now uses
   the actual last-attempt completion time and retains a one-minute floor.
2. Demand persistence started detached writers. A focused race test reproduced
   temporary-directory recreation after the caller completed. Persistence is now
   synchronous on the existing watchdog owner, serialized separately from the TLS
   lock, and remains best-effort/hourly. TLS handshakes perform no filesystem I/O.
3. Manual Retry both signaled demand and started a second network goroutine. Retry
   now only queues coalesced demand for the existing watchdog, sharing its throttle
   and cancellation lifecycle. Pending responses cannot create a second owner.

## Validation status at publication

Local Go 1.26.9:
- tlshost race suite: 100 repetitions passed after correction; original failure
  retained separately in local verification evidence.
- devicecert/netpolicy/config race suites: five repetitions passed.
- go vet for these four pure-Go packages passed.
- Added offline authenticated-request, operator demand, pending retry ownership,
  signed-out key creation and cosmetic/local-certificate UI regressions.

Full agent/UI compilation and the six-job CircleCI matrix are required on this
exact candidate before stable promotion. Local cloud lacks the GUI development
headers; focused pure-Go passes are not represented as full native acceptance.
Existing Windows test exclusions are retained, not widened. There has been no
live vendor enrollment or user-device capture during local tests.
