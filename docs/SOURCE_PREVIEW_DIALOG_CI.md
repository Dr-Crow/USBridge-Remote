# Separate native agent dialog gate

## Status and scope

This is an **implemented, locally type-checked fixture, not a passed native GUI gate**. Do not change existing package provenance to say the parent window was tested until the exact checked-out commit passes this additional command in CI. Keep the existing `agent_parent_window_interaction_tested: false` in the original manager-gate receipt: that receipt describes a different executable/test.

The fixture runs the shipped `ui.Window.ShowAndRun`, settings gear/menu, `showSourcePreviewDialog`, Stop/Close callbacks, real parent `WM_DELETE_WINDOW` close interceptor, and real Fyne StatusNotifierItem menu callbacks. It uses the native GLFW/OpenGL Fyne driver, the real verified source streamer, the real verified viewer, FFmpeg, production consent checks, fresh crypto/rand keys, and unchanged strict protocols and lifecycle code. It does not run the enrollment/service `app.App` engine or the production CLI entrypoint.

Only the parent engine facade is offline: read-only UI status methods return idle/not-enrolled state, and an unimplemented method panics instead of granting permissions, contacting a vendor, downloading a backend, or modifying an engine. A build-tagged read-only observer wraps production manager launchers and delegates to them unchanged. It neither injects keys nor replaces children, consent, random generation, timers, protocols or frames. It additionally limits the fixture to the generated `:96` display, 128×72, view-only, loopback and `/usr/bin/ffmpeg`.

All new Go production-visible symbols require the explicit `source_preview_acceptance` build tag. Normal builds are unchanged. The fixture imports Fyne's renderer-inspection helper solely to report geometry/text/state of existing widgets, then replaces its package-initialization dummy app with `app.NewWithID`. A runtime check requires the actual GLFW driver and rejects `-tags ci`. The Python driver never calls a widget callback or writes widget state: it sends native XTest pointer/keyboard events, a native WM close request, and standard tray D-Bus menu events. Widget geometry is white-box, read-only metadata; text/status assertions are not OCR evidence.

## Exact CI invocation

Apply the patch to a full checkout of the source-preview-experiment commit being tested. Use the same CircleCI Linux machine job and Go 1.26.9 toolchain as the existing preview gate. After that gate has built the pinned source package and real viewer, add a separate step:

```yaml
- run:
    name: Exercise shipped agent settings and Source preview dialog on isolated displays
    command: bash .circleci/test-source-preview-dialog.sh
    no_output_timeout: 15m
```

Put it immediately after the existing `bash .circleci/test-source-preview.sh` run, inside the same source-preview-experiment branch condition. Do not replace that command or weaken its tests. Manual equivalent from repository root, after the existing source-components prerequisites:

```sh
bash .circleci/test-source-preview.sh
bash .circleci/test-source-preview-dialog.sh
```

The second command first requires the original manager/pixel receipt to pass and its package provenance to match the current Git commit. It then consumes `artifacts/source-preview/package/components/MANIFEST.sha256` and its already verified local package. It builds only the new lightweight native parent fixture; it does not fetch native component binaries. It leaves the original manager receipt, archive and provenance unchanged. This test-only branch uploads only the allowlisted JSON receipts described below; full packages, archives and arbitrary logs are not published. A combined release-provenance step, if later added, must require **both** receipts from the same commit.

The new script installs ordinary distro test-runtime dependencies, builds the native parent with `go build -tags source_preview_acceptance`, then creates two fresh authenticated Xvfb servers. It refuses existing sockets/lockfiles. `:96` contains generated solid-blue pixels; `:97` contains both the actual parent and viewer, just as the shipped manager inherits the parent's display. No third display or test-only viewer-display override is needed.

A temporary HOME, state directory and XDG directories isolate settings. A private D-Bus session has a minimal fixture StatusNotifierWatcher so the actual close-to-tray branch is taken. It is not a full desktop tray renderer. A fresh Linux network namespace contains only loopback. The runner drops back to the CI user's UID/GID, clears capabilities, sets no-new-privileges, and passes an allowlisted environment before starting the D-Bus session, Python, GUI or media children. No account credentials, vendor tokens, host network changes, privilege grants or service enrollment are required. If a hosted runner cannot create this namespace, the gate must fail; do not fall back to the user's desktop or host network.

## Assertions performed on a successful native run

1. The actual parent has a nonblank X11-presented canvas and the settings gear opens the real Source preview menu/dialog. Independent parent sampling saves numeric color/geometry counts, never screenshots.
2. Opening the parent/dialog, filling the form, pressing Start without consent, and checking consent without Start produce no media process, manager preparation or launch. These negative observations are bounded by the test's 0.8-second windows.
3. An intentionally wrong manifest hash fails before a source/viewer launch and consumes the checkbox grant. Retry requires a fresh checkbox click.
4. All form entries are typed through native keyboard events and verified against the existing widget values. The 128×72 transport profile is selected by the shipped default. All three successful attempts use explicit `:96` capture consent.
5. A repeated native click on disabled Start does not overlap launches. Exactly one source, one real viewer, one generated-display video FFmpeg process and one private-PCM Opus FFmpeg process are present per successful attempt. Only codec-role counts and booleans are retained, never full process arguments.
6. Each attempt independently verifies actual displayed viewer blue pixels for at least five seconds with the **unchanged** `preview_pixels.py` helper. UI status/first-frame callbacks cannot satisfy this check.
7. Actual dialog Stop, actual dialog Close, and actual parent WM close-to-tray each join the source and viewer. Their observed media PIDs disappear and their observed IPv4/IPv6 TCP/UDP socket inodes disappear. The viewer window disappears.
8. The three successful attempts have different actual RIKeys, key IDs and session IDs. Only aggregate booleans/counts leave the observer; keys, hashes and descriptors are never saved.
9. Parent close hides the native window while keeping its process alive in the real tray branch. The real tray menu Open action restores it. Reopening the dialog restores an unchecked grant and empty form, with no capture. The real tray Quit callback then exits cleanly.

`result.json` sets `agent_parent_window_interaction_tested: true` only after every assertion succeeds. On failure, it records `passed: false`; forced failure cleanup does not count as successful Stop/Close. `steps.json`, per-sample pixel JSON and `last-ui.json` contain numeric/UI evidence. Logs and receipts contain no frame dumps or session secrets.

## Local verification and remaining limits

Local checks use the available cached Go 1.26.9 toolchain and dependencies without network downloads:

- Go fixture built with `-tags ci,source_preview_acceptance` as a **compile/type check only**. Its runtime native-driver check would reject this binary.
- `go test -race -tags ci,source_preview_acceptance ./internal/sourcepreview ./internal/ui`: passed on the final files; output is in `validation/go-tests-final.txt`.
- The focused dialog-locator unit test constructs the actual dialog with Fyne's test driver, but injects no capture or user input. It does not count as native parent/interaction evidence. An initial full-parent headless smoke experiment triggered races because Fyne's test driver runs fyne.Do immediately; that unsuitable smoke test was removed. The native fixture retains normal GLFW main-thread scheduling.
- `bash -n` and Python compile checks pass.
- Native build attempted locally and blocked by missing OpenGL/X11 development headers (`gl.pc`, `X11/Xlib.h`). The cloud sandbox also previously denied Xvfb socket creation; no attempt was made to bypass that denial. No native GUI run has occurred here.

Still outside this fixture: actual installed agent CLI/App engine boot, service/thin-client attachment, real user's desktop or permissions, vendor login/enrollment, stock streaming coexistence, ordinary Moonlight HTTP pairing, remote networking, input forwarding, desktop audio, larger/complex scenes, general desktop tray rendering, native Windows/macOS, and native cancellation at each distinct startup phase. Existing manager/crypto/audio tests remain separate evidence for their own scopes. This gate must run in hosted CI before any claim that the parent-dialog gap is closed.

## Temporary test-only publication scope

While the source-repository audience decision is pending, this branch publishes
only an explicit allow-list of bounded JSON receipts under `preview-evidence`.
The source-containing packages and archives built for local CI execution are not
uploaded by this increment. Existing public history is unchanged. No new source
snapshot or private-origin acceptance harness is introduced by this fixture.
A passing receipt therefore proves the test at its commit, not availability of
a newly downloadable release package.
