# Native Windows generated-media viewer acceptance

This CI-only, standard-library Go runner exercises the **actual agent CLI**
`--source-streamer-mode`, its existing `sourcestreamer.RunStdio` supervisor, the
unchanged public `2e07af3484369bc68bc8091d5a04969867eff0f9` source, the separately
hash-pinned `windows-preview-fixture`, and the actual public-Moonlight/Fyne
`source-preview-viewer.exe`.

It does not call the agent's `App.New`, enable its Windows preview Manager/UI,
exercise real desktop capture, inject input, establish a hardware presentation
claim, or authorize publishing held source/binary artifacts. Those receipt fields
remain false even after successful acceptance. The reviewed source archive pin
in the receipt identifies the frozen build input; the CI owner must separately
verify that archive and build the corresponding source executable. This runner
verifies the supplied manifest and executable hashes, not reproducible-build
provenance.

## Required CI staging and invocation

Build with the repository's pinned Go 1.26.9. This module has no external
modules and uses `CGO_ENABLED=0`. The agent, source, fixture and viewer are built
separately by the CI owner. Stage the full verified native DLL closure beside
each executable that needs it. Include source DLLs in the component manifest,
so the actual agent resolver also stages them. The runtime children deliberately
have only `System32` and the Windows directory on PATH.

Prepare the fixture's assets using its existing build/verification scripts and
use its matching compiled-in asset pins. It is a pure-Go generated-media
substitution: it never launches an FFmpeg subprocess or a desktop API. The real
FFmpeg belongs only to fixture preparation, outside this runtime job.

From this module directory on native Windows:

```sh
CGO_ENABLED=0 go test -count=1 -timeout=2m ./...
CGO_ENABLED=0 go build -trimpath -o "$PRIVATE_BIN/windows-preview-acceptance.exe" .
"$PRIVATE_BIN/windows-preview-acceptance.exe" \
  --agent "$AGENT_EXE" --agent-sha256 "$AGENT_SHA256" \
  --viewer "$VIEWER_EXE" --viewer-sha256 "$VIEWER_SHA256" \
  --fixture "$FIXTURE_EXE" --fixture-sha256 "$FIXTURE_SHA256" \
  --components "$SOURCE_COMPONENT_DIRECTORY" \
  --manifest-sha256 "$SOURCE_MANIFEST_SHA256" \
  --source-sha256 "$SOURCE_EXE_SHA256" \
  --fixture-manifest-sha256 "$FIXTURE_ASSETS_JSON_SHA256" \
  --work "$NEW_PRIVATE_WORK_DIRECTORY" \
  --output "$NEW_RECEIPT_JSON_PATH" --commit "$CIRCLE_SHA1"
```

Paths must be absolute local-drive Windows paths as seen by the native Go
program, with no alternate stream or UNC paths. In MSYS, pass `cygpath -m`
conversions as necessary. The work directory and output file must not exist;
their parent directories must exist. Reruns need fresh paths. The original
source component directory must have `manifest.json`, a Windows/amd64
`source-streamer-v1` component with the exact frozen commit as version, and all
its listed files. Pass the SHA256 of the exact `fixture-assets.json` adjacent
to the fixture executable. Never pass a production FFmpeg executable as the
fixture. Its basename must be `ffmpeg-fixture.exe` to reject an accidental
production-encoder selection. Hashes are lowercase hex.

The runner creates a new private working directory per case, retains small
source-free receipts, and leaves private runtime state for the CI owner's normal
workspace disposal. Only allowlisted receipts may be uploaded. Never upload the
work tree, generated assets, staged components, source snapshots, or binary
packages while the existing audience hold remains.

`--plan` is portable and performs no launch or native inspection. For local
validation without a Windows executor:

```sh
go test -race -count=1 ./...
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go test -c -o /tmp/windows-acceptance-tests.exe
CGO_ENABLED=0 GOOS=windows GOARCH=amd64 go build -o /tmp/windows-preview-acceptance.exe .
go run . --plan
```

Portable tests and cross-builds are **not** native execution or media acceptance.
Native unit tests additionally establish suspended launch, private pipe I/O,
natural EOF cleanup, exact Job Object membership, inherited descendant membership
and kill-on-close behavior using only test-owned helper processes. A separate
`windows-preview-process` CI job runs these native checks twenty times without
waiting for the viewer/codec build; it uses the checksum-pinned official Go ZIP
and publishes only `process.json`, with media and window claims explicitly false.
Atomic startup uses Microsoft's [JOB_LIST attribute](https://learn.microsoft.com/en-us/windows/win32/api/processthreadsapi/nf-processthreadsapi-updateprocthreadattribute).

## Gates and safe failure behavior

Each of these three cases generates a new session ID, random 16-byte key and
random key ID. Keys/descriptors travel only through anonymous stdin pipes; the
runner never writes them to argv, environment, files, receipts or child logs.

1. Render both blue and orange and observe at least two color transitions; send
   `WM_CLOSE` to the exact owned visible viewer HWND. Require `stopped.completed`
   and exit code zero.
2. Start fresh, repeat changing-pixel verification, then close the viewer's
   private stdin lease. Require `stopped.completed` and exit code zero.
3. Start fresh, repeat changing-pixel verification, then send one extra stdin
   byte. Require `stopped.failed` **and exit code zero**, because the viewer's
   current main reports lease-protocol failure through its typed terminal event.
   A zero exit alone can never pass this negative case.

Before each stop action, the runner checks that both direct children remain
alive, no viewer terminal event is already pending, the viewer lease has at least
ten seconds remaining, and the whole case is at most eighteen seconds old. This
prevents ordinary deadline completion from masquerading as the requested action.

For every case the runner requires the ordered, exact `ready`, `first_frame`,
`stopped` viewer protocol; source readiness with only the frozen encrypted
loopback/video/silent-audio/control capability set; and a completed source
terminal with nonzero bounded video and audio counters. The source lease is
closed after viewer teardown. No raw child output is saved. Unexpected stderr,
unknown/duplicate/case-aliased/null JSON fields, malformed/trailing data, missing
or extra events and oversized output fail closed.

A separate non-breakaway, kill-on-close Job Object owns each case's full process
tree. The agent and viewer are created suspended with atomic JOB_LIST membership,
verified in that job, then resumed, using an explicit three-handle
stdin/stdout/stderr inheritance list. There is no created-but-unassigned crash
window; no fallback to post-creation assignment is permitted. A native regression
exits the owning parent before ResumeThread and requires the never-resumed child
to exit without closing the outer safety job. The agent's source
and the source's two fixture instances inherit that job on creation. The job
handle itself is not inherited. An unsupported atomic job attribute or denied nested-job creation is a failure;
the runner never resumes uncontained children or changes runner security policy.
During media checks, the runner inventories only its job's PIDs, hash-verifies
their executable files, permits only those four component identities, and
requires both fixture roles to be present. Files are held read-only with Windows
write/delete sharing denied across the run. The agent-staged source is verified
again after readiness.

Window discovery binds both the exact live viewer PID and literal title. The
pixel probe revalidates ownership, asks `PrintWindow` to render only that HWND's
client area into an owned 32-bit memory DIB, and samples only its center. It does
not call screen `GetDC`, desktop APIs, BitBlt, or global input. A two-second
PrintWindow timeout, return value zero, black/stale pixels, or unrecognized
colors leaves the presentation gate failed/unverified. There is no screen
capture or software-canvas fallback. No pixel array or screenshot is persisted.
This establishes changing owned-window content through PrintWindow, not physical
monitor presentation. See Microsoft's [PrintWindow contract](https://learn.microsoft.com/en-us/windows/win32/api/winuser/nf-winuser-printwindow).

A pass separately requires viewer and agent exit zero, completion of their pipe
readers, all retained owned process handles signaled, zero remaining Job Object
members, removal of the owned HWND, and release of the source's advertised RTSP
TCP/control UDP listeners. Binding/connection checks address only those literal
IPv4 loopback endpoints. There is no firewall modification, egress isolation or
zero-WAN assertion. The full process tree's exit also ends its unadvertised
media sockets, but their individual port numbers are not collected.

`natural_cleanup` means no harness safety termination was needed. The unchanged
source itself normally cancels its generated encoder children as part of its
own normal shutdown; the runner does not mislabel this as a production graceful
FFmpeg shutdown test. Final job closure and an independent 40-second per-case
watchdog are safety nets. `safety_job_kill_used` remains separate, and forced
cleanup can never convert a failed natural-cleanup gate into a pass. A native
failure emits only the bounded partial receipt and a fixed failure code.

UI button clicking, startup cancellation, source-crash behavior and descriptor
expiry are outside this first native harness's executed cases. Unit tests cover
its protocol, pin, environment and receipt validation; do not claim those tests
establish the omitted native scenarios.
