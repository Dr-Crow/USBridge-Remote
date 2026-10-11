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
