# Inherited test-fixture observations

## Linux orphan process fixture (2026-10-08)

CircleCI Linux job 152 (agent branch commit `7c24224d`) failed
`TestKillOrphansByArgv0_FindsProcessByArgv0`: the newly started child had not been
killed after the three-second assertion deadline. The relevant test file was
independently compared byte-for-byte with upstream commit
`72071ba44425f4d53dab6fec57a02f31afe98138` before changing it:

https://github.com/USBridge-Technologies/USBridge-Remote/blob/72071ba44425f4d53dab6fec57a02f31afe98138/agent/internal/streamhost/orphankill_linux_test.go

The unchanged fixture failed 5 of 100 repeated runs in the root Linux test
environment. Synchronizing fixture readiness on the child's expected `/proc`
argv and ownership snapshot produced 500/500 successful runs. This supports a
fixture-startup visibility race; it is not proof of a new production regression
or a comprehensive orphan-cleanup audit.

The scoped fix waits for that readiness before invoking the operation under
test and always reaps the child. It does not change the production process
scanner, names matched, UID check, kill implementation, or three-second kill
assertion. The bystander test remains enabled. Nothing is skipped and a failed
readiness condition still fails the test. This adjustment was necessary to keep
CI useful for validating the fork's changes; broader upstream bug fixes remain
outside this change.

After the fixture change, both orphan tests also passed 20 race-enabled repeats;
the complete `internal/streamhost` race-enabled suite and vet passed locally.
Hosted CI remains the final platform gate for this commit.
