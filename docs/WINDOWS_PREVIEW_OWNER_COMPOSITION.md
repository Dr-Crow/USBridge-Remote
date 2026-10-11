# Windows preview production-owner composition

This increment composes the public, separately tested `windows-pipe-owner-tests`
checkpoint `1ad2302db13015e61502c261a6b95108efb4bfe4` into the viewer checkpoint
`8e1f3a98f214981b5a3a420a489d7a26880751cd`. Only the production owner, launch
adapters, manager cleanup-lease behavior and their tests are taken from that
checkpoint. The later viewer, graphics verification and signature-owner changes
are retained.

The source-streamer supervisor and preview viewer now use the same Windows
atomic suspended-start, private-pipe and Job ownership implementation. A typed
cleanup uncertainty prevents the manager from issuing a replacement capture
lease. Protocol failures and OS lifecycle failures remain independent evidence.
The Linux adapter keeps its existing process behavior, with joined writers and
bounded cleanup/error propagation.

The native process prerequisite runs the exact production owner and synthetic
supervisor/viewer adapters before the changing-pixel gate. This is followed by
the real agent CLI with the existing public frozen component and generated
encoder fixture. The ordinary Windows App.New/source-preview manager and dialog
remain disabled pending a separate native manager/UI acceptance increment.

Local validation uses an exact-copy, standard-library-only package closure
because this workspace is a partial checkout. Race tests (three repetitions),
vet and Windows compile checks pass. Hosted CI remains authoritative for the
full repository and actual Windows execution.

No private source or component binaries are introduced or published by this
increment. Receipt-only artifact publication remains in effect.
