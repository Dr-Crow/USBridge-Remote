# Engine HTTP server lifetime

This increment owns the engine's HTTP, HTTPS and optional tsHTTP Serve workers.
It does not change their listen addresses, authentication handlers, certificate
selection or TLS minimum. Constructing App still does not start these servers.

Run registers every server generation before starting Serve. A bind/Serve error
is retained and cancels the engine; Run rolls back already-started components.
All owned HTTP generations share one five-second shutdown deadline. The first
cleanup uncertainty is retained in Run's result, even if a later observer sees
retirement. Serve workers and active outer handlers must join before a clean
result. A deadline invokes Server.Close and remains a failed cleanup result.

Hot HTTP/HTTPS application retires the exact old generation with the existing
three-second replacement budget. A per-role replacement slot prevents concurrent
replacements. Final engine shutdown seals registration, so no replacement can
open a listener after the final inventory. Fully joined retired generations are pruned, keeping only the current role
generations; prior errors are retained separately. Failed retirement never
admits a successor. Pending external-listener startup is also owned.

## Explicit remaining request-worker scope

net/http stops managing connections after Hijack. This owner tracks its own
connections, closes observed hijacks on retirement, and joins their outer
handlers. It deliberately returns `http owner: hijacked worker cleanup unverified`
if any hijack occurred, because these existing routes still have work to finish:

- mouseWS and clipboardWS stop ping workers without joining them
- legacy browser USB attach waits for only one relay direction
- browser gamepad/pen handlers close their synthetic session; those nested
  session lifecycle guarantees need explicit composition tests

No full-App/request-worker readiness claim is made for those sessions. The next
focused changes must add the route-specific joins and synthetic/loopback tests,
without changing input, clipboard, device-consent or authentication behavior.
Other engine watchdogs and global log watching are outside this HTTP-only proof.

## Tests

`go test -race -count=100 http_owner.go http_owner_test.go` in internal/app tests
HTTP+HTTPS+ts role retirement, active request cancellation, partial startup
rollback, expired cleanup, hijacked-connection uncertainty, hot replacement,
replacement/final-seal races and rejected external-listener retirement. TLS uses
an ephemeral in-process test certificate; no host trust store is changed.

Native full-agent CI remains required before this increment can be integrated.
