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

## Native console-host finding (2026-10-10)

The first native inventory gate failed: `CREATE_NO_WINDOW` added an owned
`System32/conhost.exe`, beyond the expected helper processes. The exact same-query
classification identified that system host without disclosing process IDs or
paths. A separate `DETACHED_PROCESS` comparison passed all 20 native repetitions.
The runner now starts pipe-only children detached from any console, retaining
atomic Job Object assignment, suspended membership verification and the three
explicit pipe handles. Tests still require exact process identities and counts;
no unknown or system-console process is exempted from inventory or cleanup.

The generated encoder fixture is linked as a Windows GUI-subsystem pipe program
(`-H=windowsgui`), so the frozen source's ordinary encoder spawn cannot allocate
a console for this CI-only substitute. This is an explicit generated-fixture
build choice, not evidence of production FFmpeg console behavior. The source
itself remains a console-subsystem build; the actual agent supervisor now starts
it with `DETACHED_PROCESS`. Private current-source encoder launch changes and
production desktop acceptance require their separate private integration gate.

The subsequent native gate reached a distinct assertion: closing the kill-on-close
job ended the direct helper with exit code zero. Microsoft's kill-on-close
contract guarantees termination, without promising a nonzero code. The runner
therefore records its own safety-close action and rejects `wait` as non-natural
regardless of the kernel exit value. The descendant test still requires both
owned processes to terminate; portable tests explicitly prove that safety close
plus exit zero cannot pass. A normal successful case must finish and empty its
job before the safety handle is closed.

## Early owned-window prerequisite

`--window-startup` accepts only the verified viewer, its SHA256, exact commit,
new work directory and new receipt path. It starts the same GUI executable with
its normal `--source-preview-stdin` entry, a fresh private descriptor and an owned
inert loopback listener. No agent, source, encoder, capture or input process is
started. Exact PID, title and visibility must pass, followed by natural process
exit and complete empty-Job proof. The listener supplies no media. This gate runs
before expensive component compilation; full changing-pixel and cleanup cases
remain mandatory afterward.

The viewer now checks its Fyne NativeWindow HWND immediately after Show, before
network/renderer startup. A failed prerequisite emits stopped/failed with an
optional closed `failure_code`; successful events are byte-schema compatible.
Older strict supervisors reject this early failed startup. Diagnostics retain
only fixed graphics categories from at most 64 KiB of pre-network Go log writes;
no native text, driver identity, paths or credentials are emitted. No window of
another PID is queried through this viewer check. Native CI must establish the
actual failure category; an earlier typed first_frame event is insufficient
proof of native window creation or visible playback.

The public Go build cache may be copied one-way into the unsaved job-local cache
only after its exact key/toolchain/package/source and all content hashes match.
There is no cache save or copy-back after component compilation. A source-free
public-cache-seed.json records counts, hashes and unchanged public input.

### Software-OpenGL acceptance after the native window diagnosis

At public agent commit `20dd9cd4fbe767a57e67151f98bdb7c3225cfb65`, the
same-executable startup gate reported `graphics_api_unavailable`, zero visible
owned windows, and a natural exit with an empty Job. This is a real graphics
prerequisite failure, not a successful viewer/pixel result. The exact-key public
cache reduced the native viewer/privacy stage from 17m44s cold to 1m51s warm.
The one-way cache seed separately verified that its public source was unchanged.

The next gate stages the official MSYS2 UCRT64 Mesa 26.2.4-1 archive, pinned to
SHA256 `82a30042848b6393f2a21cdee66b164e4cf4fe15a9721a5f1d1c7280e004ebdc`.
Only its two exact hash-pinned WGL DLLs and recursively verified UCRT dependency
closure are placed beside the disposable viewer. Runtime dependencies come from
the official MSYS2 registry; their installed versions and each staged DLL hash
are recorded. Existing compiler/package versions must remain unchanged. This
happens after the public cache has been saved and copied one-way into the
unsaved job-local cache. No OS/GPU driver, registry entry, machine environment,
security setting or host device is changed.

The Windows viewer removes inherited Mesa/Gallium/driver diagnostics and sets
only the fixed process-local `GALLIUM_DRIVER=llvmpipe` and
`LIBGL_ALWAYS_SOFTWARE=true` policy before constructing Fyne. Mesa 26.2.4 reads
these through the Win32 environment. Linux keeps its existing graphics policy.
There is no new arbitrary environment or viewer protocol input.

A separate, explicitly tagged `graphicsprobe` test launches a small C WGL probe
inside the existing owned Job. It creates only its own hidden window/context,
reads bounded ASCII GL vendor/renderer/version strings, requires llvmpipe,
verifies its PID-scoped loaded modules against the pinned app-local closure,
then requires EOF, natural process retirement and a complete empty Job. Its
receipt says `actual_viewer_tested:false`; it is never counted as pixel proof.
The actual normal viewer entry then independently passes the same owned-title,
visible-window, changing-pixel and natural-cleanup gates. Both actual startup
and media cases verify the viewer PID's loaded Mesa module paths and report
only basenames and hashes. Unknown locations, hash changes, missing Mesa roots,
API failures and safety termination remain failures.

Sources: https://packages.msys2.org/packages/mingw-w64-ucrt-x86_64-mesa and
https://docs.mesa3d.org/drivers/llvmpipe.html . Only source-free JSON receipts
are published. This software-rendered CI profile is not GPU acceleration or
physical monitor presentation validation.

The first software-staging run at `c37d773` failed before WGL execution because
Windows selected BSD tar, which does not support GNU `--force-local`. The
correction uses Python 3.14's native zstd/tar reader, checks each exact regular
member and payload hash, and never extracts archive paths or invokes tar.
The same run showed the rolling registry had advanced to LLVM23. Mesa's pinned
build imports LLVM22, so the correction stages only `libLLVM-22.dll` from the
official LLVM22.1.8-3 archive (SHA256
`b22437a27246bf17447061d5c7faea5f0e457aa9a65b89dad692fe767afb1d6a`), rather than
installing or aliasing a different ABI. Its exact DLL hash is
`ecef91d79184533faa2d74d1965c0737843f4c2e02c7cb8dd3309a6c97ddec9b`.
These staging repairs do not establish a WGL or visible-pixel pass.

At `1b1b781`, staging and the owned WGL probe passed with Mesa 26.2.4,
llvmpipe/LLVM22.1.8 and natural cleanup. The normal viewer then created its exact
owned visible window, but the loaded-module inventory rejected a location
outside its verified closure. This failure preceded media/pixel execution and
required safety cleanup. The follow-on diagnostic emits only the first rejected
DLL's bounded basename, a closed location category and whether it was declared
in staging. It preserves the original fatal code and every module acceptance
rule. A Windows side-by-side location is a diagnostic hint, never an exemption.

The `2789ddd` native receipt identified the rejected module as `gdiplus.dll`
in Windows side-by-side storage. The next diagnostic locks only that observed
file, verifies its resolved handle path and SHA256, then invokes the installed
Windows PowerShell signature verifier inside a separate bounded owned Job.
It records the existing catalog-aware signature status, `IsOSBinary`, bounded
certificate names/fingerprint and matching file hash. Full paths stay on private
pipes. No certificate, revocation, execution-policy or system setting changes
are made. The verifier must naturally exit and leave a complete empty Job.
This is evidence collection only: even a Valid signature keeps the module
rejected, and cannot establish pixel or natural-viewer-cleanup acceptance.
Signature validity and certificate display names alone are not authorization
or an independently established publisher trust decision.

The first exact-file inspection at `0b01ca6` verified the observed final path
and SHA256, but obtained no signature result. A mandatory, device-free native
preflight now exercises the same private-pipe verifier against the existing
System32 `kernel32.dll` before the large viewer build. Its closed receipt records
signature/result stage, exit status and elapsed time; raw exception text and
paths remain private. This isolates verifier defects from window/media work.
