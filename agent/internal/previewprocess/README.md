# Windows pipe-child ownership

This standard-library package owns the private-pipe preview launch adapters.
The Windows settings action remains separately gated on its native Manager and
UI acceptance; this package does not enable that action.

`Start(ctx, Spec{Path, Args, Env, Dir})` creates a suspended, detached process
already assigned to a private kill-on-close Job Object, verifies ownership, then
resumes it. Exactly three pipe endpoints may be inherited. Caller-side pipes,
Job and retained process handles are noninheritable. There is no shell, elevation,
alternate identity, breakaway, desktop manipulation or fallback.

`Process` exposes private stdin/stdout/stderr plus `Done`, `Wait` and bounded
`Stop(grace)`. Exit status and cleanup cause are separate: a zero exit never hides
forced termination. A retained process handle observes root exit independently of
output and remains owned until the tree retires. Residual descendants are stopped;
invoked pipe closers and native handle cleanup are joined within the cleanup budget.
Natural output remains caller-owned so unread bytes can be drained.

Callers must still verify executable hashes, filesystem/reparse boundaries,
environment policy, consent and session eligibility. This contains cooperative
child lifecycles; it is not a hostile-code sandbox. Native API availability fails
closed, inventory is capped at 128 members, and unrelated unrestricted-inheritance
launches in the same parent are outside this package's guarantees. Native kernel
calls cannot be made universally interruptible. Stop has an independent bounded
caller deadline and reports incomplete cleanup honestly.

The native test suite uses only copies of its own generated test executable. It
covers private pipes, noninheritance, root/descendant retirement, cancellation,
blocked I/O, parent exit before resume and repeated handle cleanup. No window,
media, desktop capture, audio device or input API is used. Cross-compilation is
not native runtime acceptance; CI must verify the exact package commit first.

## Startup release accounting correction

The earlier implementation discarded Close errors for the parent's three
inherited-pipe copies and original process/thread handles on successful Start.
Historical receipts still prove their independently observed pixels, root exit
and Job inventory. They do not prove release of every startup-only handle.

Startup now closes/checks each parent child-pipe copy before Resume, then checks
the original thread/process handles before publishing Process or starting its
wait/lifecycle workers. A failed release keeps the duplicated process handle and
Job under the startup owner, terminates and waits for that exact child, and
returns a typed containment uncertainty. It never retries an uncertain native
close, which could otherwise close a recycled handle. Failed acquisition and
rollback pipe/Job releases also preserve uncertainty alongside the primary error.
The test-only per-call observer injects errors after real closes; it cannot skip
or replace a native close. Spawn flags, private pipes, Job limits and ownership
admission are unchanged. Native failure-injection acceptance is required before
using this correction for new catalog evidence collection.
