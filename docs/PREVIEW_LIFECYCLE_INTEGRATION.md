# Coherent preview lifecycle checkpoint

This experimental integration combines the public agent-owned lifecycle changes
already accepted separately, without changing component snapshots or enabling the
Windows settings action:

- Base preview: `d16a14a90cd9a3709c2b58bcdf1c08074910d50c`, whose Linux
  actual settings/engine and Windows generated CLI renderer gates passed.
- Route workers: `db6fb29fc545d26e32b7ef358f5e13fbbca58e30`.
- Diagnostic signal/admin Serve workers:
  `740382d7fbe70a803031e3b80c8ee5fbc61df556`, all six ordinary gates passed.
- Windows startup handle-close accounting:
  `c79bd90f46b95082b66c44d442a1979b715cafed`, native owner and launch/lease
  adapter gate532 passed. The owner receipt and consumer share one reviewed test
  manifest and its exact checkout hash.

The new combined revision must itself pass the three preview workflow jobs.
Prior checkpoints do not establish combined acceptance. Both Linux and Windows
compile the complete App and repeat the focused bridge, HTTP, route, signal and
admin ownership tests before their actual preview lifecycles. Linux additionally
runs the real settings dialog and CLI/App.New inside the existing isolated
container. The Windows media gate remains CLI-only generated input; Manager
staging/unit evidence does not establish ordinary non-elevated Manager execution.

Historical pixel observations and measured root/Job retirement remain valid.
Before c79, successful Start did not propagate the parent's startup-only pipe,
thread and process handle-close errors. The new owner fails closed on those
uncertainties, preserving the original failure and owned rollback path. This
narrow correction does not establish all-resource retirement of an entire App:
clipboard callback leases, admin request callbacks and other watchdogs remain
outside the established ownership scope.

Only source-free receipts may be published by this experimental workflow. Current
private reconstructed components, their source archives and their binaries are
not copied into this integration. Auth, explicit consent, local policy, existing
Windows eligibility checks and exact OS-module trust gates remain required.
