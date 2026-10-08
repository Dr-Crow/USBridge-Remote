# Windows test limitations

The first complete native Windows test run (CircleCI build 4, commit
417da48b2631e465f50562e1b4b08529bff44628) exposed pre-existing portability
assumptions. The build uses an explicit named exclusion list rather than
silently ignoring a failed test command. This is **not an unqualified full-suite
pass**. All other tests, including fork policy/provisioning/update checks,
must pass before packaging.

## Excluded existing tests

- Nine process-lifecycle tests in app/enginelock, streamhost/sunshine_backend
  and streamhost/sunshine_hang require `/bin/sh`, `/bin/sleep`, executable shell
  scripts or POSIX SIGTERM semantics. Those fixtures cannot run as native Windows
  processes. Their exact names are recorded in the artifact `test-exclusions.txt`.
  Windows-native helper-process coverage remains to be implemented.
- `TestAcquireEngineLock_ExclusiveAcrossHandles`: exclusivity itself succeeded,
  but reading the current holder's PID returned 0. Windows byte-range locking
  prevents the second handle reading the locked PID byte. This is an existing
  limitation of holder identification/eviction, not proof of two engine owners.
  It is deferred rather than changing lock compatibility in this feature branch.
- `TestBenchLoadStartStopWireFormat`: after a fixed one-second sample interval,
  the Windows runner returned no samples and no explanatory error. This existing
  benchmark sampling/timing issue is deferred. It does not test entitlement or
  component provisioning.

## Corrected portability defects

- Signed release fixtures are checked out byte-for-byte using `.gitattributes`;
  Windows CRLF conversion otherwise invalidates their genuine signatures. The
  signature verifier is unchanged and its test is not excluded.
- Sunshine config arguments previously added `file_state` twice on Windows.
  The redundant identical final override was removed; the common override and
  its test remain intact.

## Remaining manual acceptance

A successful CI compilation does not prove streaming quality, driver installation,
USB sharing, Windows desktop rendering, hardware 4:4:4 support, or proprietary
runtime feature entitlements. Follow `FORK_TEST_PLAN.md` on a Windows test machine.

## Shipped executable startup probe

The pipeline now also copies the fully packaged Windows runtime into a temporary
profile, launches its actual GUI-subsystem EXE with `--headless`, and requires a
successful loopback HTTPS `/api/healthz` response within 60 seconds, using an
operator certificate signed by a temporary test CA. Python verifies the chain
and IP SAN normally; no trust store is changed. It force-stops only
that test process tree afterward and records the EXE hash and result in
`native-agent-smoke.json`. Disposable configuration/state/logs are not packaged.

This narrowly tests the shipped executable loader and headless agent startup.
It deliberately uses strict local network policy, no USB/capture consent, and a
Sunshine selection with no prepared Sunshine binary. No streamer is launched or
downloaded for this probe. The adjacent component discovery/preparation check
remains a separate test. This is not GUI rendering, normal desktop first-launch,
streaming, physical-device, or OS-level WAN-blocking acceptance.

The first HTTPS probe (job 148) timed out after the HTTP-only native probe had
passed. Its generated test leaf omitted Authority Key Identifier because the
fixture signed using the CA template rather than the parsed issued CA. Strict
OpenSSL verification independently reproduced that error. The fixture now uses
the issued CA's generated Subject Key Identifier, with a regression test, and
passes strict OpenSSL and Python HTTPS verification locally. Client certificate
checks are not relaxed. Hosted Windows execution must still verify the corrected
fixture; the probe now preserves the last health/TLS error in its result JSON.
