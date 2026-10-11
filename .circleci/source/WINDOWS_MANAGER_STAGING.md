# Windows Manager staging checks

The source-free suite uses synthetic, non-executable files. It verifies exact
source/viewer roles, hashes, full dependency membership, path/reparse rejection,
identity, bounded copies, mutation detection and atomic non-replacing publication.
It does not execute a component or establish Windows Manager/viewer readiness.

## CPython Windows timestamp compatibility

The first native run at42eb failed the path/handle metadata comparison. The
captured comparison diagnostic at f40458a/job408 confirms four failures have
equal identity, size, write time and birth time, with only ctime differing. Path
ctime equals birth time and handle ctime does not. No predicate was disabled in
that diagnostic run. CPython documents distinct `st_ctime_ns` meanings for path stat and
fstat on Windows3.14 in https://github.com/python/cpython/issues/157671.

The compatibility fix compares device, file ID, size, write time and explicit
birth time across the Windows APIs. Birth time is required, never defaulted.
Full path stamps (including path ctime) are compared before/after and against
inventory snapshots. Full handle stamps (including handle ctime) are compared
before/after reading. POSIX comparisons retain ctime semantics. SHA-256 of both
input and copied bytes plus a second complete input inventory are still required.
Regression tests retain identity/size/write/birth mismatches, missing birth time,
and a handle-side metadata change as failures. No tolerance, timestamp rounding,
case-insensitive dependency allowlist, symlink normalization or retry is added.

## Exact WGL probe retirement before Manager staging

Job412 at b4df932 passed all three CLI viewer pixel/cleanup cases, then correctly
rejected the second source-owned EXE (`windows-graphics-probe.exe`) sharing its
viewer dependency directory. The preparer now requires the exact probe SHA and
its successful software-only, natural-empty-Job receipt, then atomically moves
that one file to a fresh sibling holding directory. Its identity/hash are checked
before and after. No file is deleted, no unknown member is ignored, and the
unchanged component stager still requires exactly one EXE plus pinned DLLs.
Wrong hashes, forced/unproven cleanup, symlinks and pre-existing destinations
fail closed. Remaining unknown extras are tested to remain fatal.

## Size-bound diagnosis after job 422

The same-SHA rerun restored the early window gate and passed all three CLI media
cases, then the unchanged stager rejected `file_size_limit`. The prior failure
receipt does not identify which input reached its bound. The next diagnostic
adds only a closed component role, member kind and integer observed/limit byte
counts. It contains no path, basename or contents, and keeps every existing
32 MiB probe, 64 MiB executable, 256 MiB DLL and 768 MiB aggregate bound unchanged.
No larger input is admitted based on a successful viewer run alone.
