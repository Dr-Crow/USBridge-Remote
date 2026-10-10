# Windows pipe-child ownership

This standard-library package is a preparatory lifecycle primitive. It is not yet
used by the preview Manager and does not enable the Windows preview UI.

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
