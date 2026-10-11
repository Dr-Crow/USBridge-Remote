# Route-owned WebSocket workers

Mouse and clipboard WebSocket handlers now close their socket before joining
both their ping worker and request-cancellation watcher. The legacy browser USB
attach route binds its five-second broker dial to the request context and joins
both byte-relay directions after either endpoint ends. Authentication, origin
validation, input/clipboard permissions, wire bytes, broker address selection and
physical-device operations are unchanged.

The route-local helper closes canceled handoffs synchronously, closes each
transport once, and retains substantive close errors even when joined with an
already-closed sentinel. Socket closure precedes waiting on workers; no join is
performed while holding a writer mutex. Mouse held-button release remains after
the joined ping worker and continues to respect per-connection ownership.

Tests use in-memory transports, ephemeral loopback WebSockets, synthetic
clipboard backends and stub input. They cover blocked writes, parent cancel,
ordinary EOF in either relay direction, canceled handoff, repeated/concurrent
retirement, close failures, unchanged payloads and auth/unavailable responses.

This is a focused route-worker improvement. Clipboard manager callbacks can
still be active after unregister and an old connection can unregister a newer
connection. The global blob reaper and browser-session exporters also have
separate lifetimes. HTTP shutdown therefore continues to report hijacked worker
cleanup as unverified; these joins alone do not establish full App retirement.

The first aggregate run exposed an existing clipboard test startup race. The
backend can change between its initial best-effort file enumeration and payload
read, so a newly appearing initial payload need not have a pending announcement.
A deterministic reproduction used unchanged manager source. The polling test now
waits for the empty initial read before changing its synthetic backend; a separate
prepopulated test verifies initial pending-before-real ordering. Both retain the
same event assertions. This does not introduce an atomic OS-clipboard snapshot
contract or change the production best-effort progress announcement semantics.
