# signatureowner

A local, standalone Go 1.26.9 standard-library extraction of the public agent's
signature-only Windows process owner. It has no CLI and no external modules.
See `PROVENANCE.md` before reuse. This directory has not been published.

## Contract

Use `Run(ctx, spec, input)` for one source-reviewed, hash-pinned PowerShell query.
It requires the exact installed System32 PowerShell plus one exact System32
conhost, private inherited stdin/stdout/stderr handles, atomic Job assignment
while suspended, readiness before input, no unknown descendants, and complete
natural cleanup. It is not a general shell runner or a trust-decision engine.

The trusted caller stages an immutable `.ps1` in a private existing absolute
Windows work directory, provides exact lowercase SHA256 pins for that script and
both installed system binaries, and optionally pins direct-child assembly/data
files. Pins must originate in reviewed source or validated build evidence, never
in an acquired image or request. Each script variant needs independent review.
The source-owned `SignatureScript`/`SignatureScriptSHA256` pair reproduces the
original catalog-aware `{ "path": "C:\\..." }` query.

`Spec.Timeout` includes preparation, startup, input, the entire query/batch and
retirement. It defaults to 30 seconds; the hard maximum is 30 minutes. Context
cancellation shortens it. The 5-second startup prerequisite and 3-second input
write cap remain separate shorter limits. There is no per-file timeout reset.
`Run` performs bounded forced cleanup on failure and clears partial output.

Only `err == nil` plus a complete `Result`, including `ResourcesReleased`, establishes the ownership protocol.
`Result.Output` is one bounded JSON object and can contain a query failure result.
The caller must validate its exact schema, queried file identity/hash and trust
policy. A valid OS signature or zero process exit alone is not authorization.
The owner returns no raw stderr, process IDs or paths in its receipt.

This is an ownership validator for trusted scripts, not a sandbox. Scripts must
not compile, spawn processes, or execute acquired images. Changes to certificate,
revocation, machine security or execution policy are outside this package.

## Aggregate query and release contract

The 4096-byte request/result caps are intentional and never silently truncated.
A reviewed batch caller can stage a bounded inventory as a hash-pinned dependency
(for example, at most 2048 entries and 4 MiB), send only its hash plus bounded
schema/identity fields, and return only fixed counters and hashes. If the reviewed
script writes a larger bounded receipt in the owned work directory, the caller
must validate its exact count, order and input/hash binding before accepting it.
This keeps the original one-line process protocol and one absolute deadline.

Every acquired file/pipe/process/Job close is accounted for. Failed resource
release returns typed `ResourceCloseError`, preserves an existing primary error
for `errors.Is`, sets `ResourcesReleased` false and suppresses result output.
Raw OS errors and paths are not copied into that typed diagnostic. Native test
workspaces use caller-owned temporary directories and are retained if worker,
watchdog or resource release is uncertain.

## Verification

Portable:

    go test -race -count=1 ./...
    go vet ./...

Cross-compilation (does not execute Windows code):

    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test -c -o signatureowner.test.exe
    CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test -tags signatureownernative -c -o signatureowner-native.test.exe

Native prerequisite on an authorized Windows executor:

    go test -count=1 -timeout=5m -tags signatureownernative ./...

The original c451d77 native receipt is provenance evidence only. This extraction
and any separately reviewed caller script have not received native execution
proof in the local Linux work session.
