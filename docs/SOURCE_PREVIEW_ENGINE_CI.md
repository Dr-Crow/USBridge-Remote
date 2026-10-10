# Real CLI/App.New native preview gate

This additive gate is distinct from the unchanged manager and native dialog
fixture gates. It runs the actual `cmd/usbridge_agent --strict-lan`, owning the
real `Start → App.New → Run → Window.ShowAndRun` engine. It never calls App.New
repeatedly inside a test process or substitutes an engine, permissions provider,
manager, launch function, clock, random source, or protocol. The fixture keys
created by the real app exist only in disposable private state.

## Hosted CI step

On the existing Linux machine job, after both the original source-preview and
source-preview-dialog steps have passed for the same checkout:

```yaml
- run:
    name: Verify real CLI engine and native preview in a disposable unprivileged container
    command: bash .circleci/test-source-preview-engine.sh
    no_output_timeout: 15m
```

The patch adds exactly this step to the source-preview-experiment branch. The
runner requires Go 1.26.9, existing native build dependencies, Docker, and the
already-built `artifacts/source-preview/package/components` with passing
same-commit pixel provenance. It performs no source fetch and copies only the
verified source-streamer and source-preview-viewer binaries and their unchanged
manifest from that package. No broker binary, source tree, archive, or credential
is copied. The prior package's pinned public source commit must remain
`2e07af3484369bc68bc8091d5a04969867eff0f9` and viewer client pin
`a232e27d5c423eb8e7de8da91f0eefcf9171348f`.

The exact reviewed base is
`ubuntu:22.04@sha256:08ea48a03a3e78ebc7cd526e6a275053223aadd88bfc09cc49b06d5281525fde`.
Its independently verified config digest is
`e8df6c74f650ad385efe2e20103eca9b0ad50690fb3c63d49fa00762412bbf55`,
identifying Linux/amd64 and Ubuntu 22.04. No mutable tag is resolved or pulled.
Jammy is required by the already-built viewer's FFmpeg ABI: libavcodec.so.58,
libavutil.so.56 and libswscale.so.5. The reviewed Noble cimg base supplies different
major library versions and cannot be substituted without changing the inputs.

Only ordinary official Ubuntu runtime dependencies are installed at image build
time, as root; runtime still uses UID/GID 10001. The strict numeric receipt records
`base_image_sha256` (the fixed manifest above, equality-checked) separately from
`runtime_image_sha256` (the actual built runtime image identity). The temporary
image is never pushed or saved, and is removed when the step ends. Runtime is
fully offline. The base is immutable; apt dependency versions are not locked, so
this is not a byte-for-byte reproducible dependency-build claim.

## Isolation and instrumentation

- Docker `--network none`: private loopback only, no non-loopback interface or
  default route, explicit failed external-connect probes.
- Private PID/proc, mount, IPC, UTS and network namespaces, verified against the
  outer runner. No user-supplied host data, directory, device or socket mounts, including
  no X11, D-Bus, audio, Docker sockets or home directories. Docker supplies its
  ordinary generated host/resolver metadata and init-binary mounts. No supplemental groups or device grants.
- Unprivileged UID/GID 10001, all capabilities dropped, no-new-privileges,
  default seccomp, read-only root, resource/PID limits.
- Private `/tmp`, `/run`, and mode-0700 `/work` tmpfs, with a separate empty
  root-owned 1777 `/tmp/.X11-unix` tmpfs for unprivileged Xvfb socket creation.
  Docker defaults tmpfs mounts to `noexec`, including `/work` when that option is
  omitted. This blocked the original fixture's production staging path. Following
  explicit approval, only `/work/state/source-preview` has a separate executable
  tmpfs: exactly 256 MiB, mode 0700, UID/GID 10001, `nosuid,nodev`. Its `/work`
  and separately mounted `/work/state` parents remain `noexec`, as do `/tmp`
  and `/run`. The state parent is 64 MiB and mode 0700 under the same user.
  The real resolver still copies and rehashes both executables beneath
  `/work/state/source-preview/local-components`; there is no `/opt` or symlink
  bypass. Preflight reads actual kernel mount flags, file ownership/mode and
  filesystem capacity; the closed `private_mounts` receipt preserves those
  values and rejects any unexpected executable mount or larger capacity.
  Docker removes tmpfs state, including new master keys, at container exit.
- Both authenticated Xvfb displays are created inside the container. Only :96
  receives generated 128×72 blue pixels. The actual GUI/viewer runs on :97.
- The plain native CLI is built without any acceptance tag and must boot visibly,
  bind its real HTTP/admin/USBPass listeners, remain idle for 16 seconds, and exit
  through its real no-tray WM close path without ever writing observer metadata.
- The second binary is the same shipped command, built only with
  `source_preview_engine_acceptance`, never Fyne's `ci` test driver. This tag adds
  a read-only hit-target/engine-metadata observer to the actual window. Its hook
  is a no-op in ordinary builds and without explicit runtime opt-in. It does not
  change configuration or the production preview manager. The receipt explicitly
  identifies this instrumentation. A private StatusNotifierWatcher enables the
  real tray D-Bus menu; it does not claim desktop tray rendering coverage.

Native actions use XTest pointer/keyboard events, WM_DELETE_WINDOW, and the real
tray's D-Bus menu API. Parent selection uses xdotool's explicit AND (`--all`)
across PID and exact escaped title, plus `--onlyvisible`, and rejects ambiguity.
Only startup may retry a missing match. Close-to-tray visibility is checked using
the previously verified parent XID, never a newly selected hidden GLFW window. Direct widget callback invocation is never native gate
proof. The separate locator unit test may instantiate controls without starting
an engine, and its scope remains only unit coverage.

## Assertions

The real engine uses strict LAN/runtime-local policy, disables the rekeyed local
runtime, TLS, Tailscale, clipboard sync, stock streamer/USB consent, and starts
without account, entitlement, component source or persisted keys. Missing stock
Sunshine is intentionally a degraded idle state, not a functioning stock stream.
No permission, installation, login, pairing or backend-switch control is clicked.
The instrumented process lives beyond 77 seconds, covering the normal post-minute
missing-Sunshine retry opportunity. Stock/account/session state is repeatedly
checked; Docker provides the hard no-egress boundary, not sampling alone.

The real settings → Source dialog must reject Start without capture approval,
remain idle when only the checkbox is selected, reject a wrong manifest and
consume that grant, then require a new explicit grant. Four successful runs each
show at least five seconds of independent native pixel samples using the
unchanged `preview_pixels.py`; a status or first-frame callback is insufficient.
The unchanged public process classifier requires one source, one native viewer,
one exact :96 X11/128×72 video FFmpeg encoder and one private-stdin s16le/Opus
encoder. Their raw arguments, descriptors, private pipes and pixels are never
recorded. Repeated Start must not create overlapping children.

The four terminal actions are Stop, dialog Close, parent close-to-tray (then real
tray reopen), and real tray Quit while the fourth preview remains active. Every
known media PID and socket inode must disappear before a passing assertion.
Idle and terminal checks also census the entire private PID namespace, so a
media process reparented to init before the captured PID snapshot cannot escape
cleanup assertions. Live media must remain descendants of the actual agent.
Stop/Close return to the real agent's baseline loopback listeners; USBPass is
unconditionally created by App.New even without USB consent. Only actual CLI
process exit must release that USBPass listener and the HTTP/admin listeners.
A stale admin socket pathname can outlive the live endpoint: this gate proves
process/OS socket cleanup, not that every cancellation goroutine fully completed.
Forced termination/container destruction is failure cleanup and never pass proof.

## Publication and limits

The new gate publishes only `source-preview-engine/result.json`, through the
existing source-free receipt collector. An exact closed schema permits booleans,
bounded integers, SHA hashes, and one fixed instrumentation identifier. Unknown
fields, free-text diagnostics, keys, descriptors, source or binary artifacts are
rejected. All private runtime logs/configuration/authentication stay in tmpfs;
only a bounded last-completed-stage number and closed diagnostic categories leave
a failed container. Diagnostics contain owned code locations, a fixed exception
category/UI phase and bounded child counts; never exception text, raw paths,
arguments or application logs. Stages
1–2 are plain startup/close; 3 is observed idle; 4 is consent/manifest rejection;
5–7 are Stop/Close/close-to-tray; 8 is post-cooldown idle; 9 is active fourth
preview; 10 is verified CLI exit. Existing fixture and
manager receipts retain their original scope.

No Docker or Xvfb execution was performed in the development sandbox. Hosted
runtime is a required remaining gate. Native compilation may require the hosted
GLFW/OpenGL/X11 development headers absent locally. Local syntax, schema,
locator, typechecking and race tests do not count as live native evidence.

Even a passing hosted run does not prove AppImage/distribution installation,
real user's desktop or devices, hardware permission grants, KMS/Wayland, active
stock-stream coexistence, enrollment/pairing, real captured audio/input, remote
networking, complex content, Windows or macOS production parity.
