# Owned browser USB relay lifecycle

This agent-only change moves the stock browser relay's loopback listener out of
`App.New` and into `Run`, before streamer startup. It does not enable a USB device
or change broker consent, HMAC/session authentication, driver installation or the
existing byte-level protocol. The independently reconstructed broker is not part
of this change.

The owner closes the listener and all admitted connections, then joins Accept
and every connection handler. The ATTACH/API relay joins both copy directions
before returning. A canceled dial is tied to the engine context. Preambles have
the existing 256-byte bound enforced by ReadSlice plus a five-second read limit;
up to 64 concurrent connections are admitted. A blocked/failed cleanup returns an
error and does not claim successful retirement. The one final engine join has a
five-second bound; it is not reset for each connection.

An optional bridge listen failure retains the previous degraded engine startup.
Its closed `browser_bridge_state` is `unavailable`; it never becomes `ready`
merely because the native USB broker is alive. Status exposes `not_started`,
`unavailable`, `ready`, `closing`, `closed` or `cleanup_failed`, without raw error
text or internal addresses. The existing GUI broker row explicitly notes an
unavailable browser bridge. Native broker/legacy paths retain their own status.

Tests cover failed and canceled startup, canceled ownership handoff, repeated
Close, racing Accept/Close, incomplete joins, close errors, bounded admission and
preambles, both relay directions, buffered API bytes and engine-start rollback.
They use loopback sockets and in-memory streams only; no physical USB or drivers.
The API package and portable owner/rollback tests can run independently. Full
App/Fyne compilation and native Windows runtime remain separate CI requirements.

This fixes one constructor-owned resource. It is not proof that all remaining
App.New state/configuration changes or all engine background workers are joined,
nor is it Windows Manager or complete GUI acceptance.
