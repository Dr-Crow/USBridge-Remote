# Experimental same-host source preview viewer

This is a child of the agent's locally authorized source-preview session manager,
not a fourth stock backend or a GameStream HTTP/pairing adapter. Version 1 is
native Linux/X11, view-only, H264 4:2:0 SDR and synthetic silence. The default
approved profile is 128x72 at 30 fps for at most 30 seconds. The descriptor
validator permits up to 640x360, without claiming that larger profiles have
passed native acceptance. It never automatically reconnects. Source and viewer
binaries must both be hash-pinned by the manager before launch.

## Private child protocol

Launch `source-preview-viewer --source-preview-stdin` with private stdin/stdout
pipes. Stdin must be a pipe. Send one newline-terminated JSON object, at most 4096
bytes including the newline, then keep stdin open for the lease. EOF or any
additional input byte stops the viewer. Do not pass keys through argv,
environment variables, files, status responses or logs.

Required exact v1 keys are `schema_version` (1), `profile` (`source-preview-v1`),
`session_id` (16–128 ASCII letters/digits/underscore/hyphen), `rtsp_url`
(`rtspenc://127.0.0.1:<dynamic port>` with no path/query/user/fragment), `key_b64`
(standard strict base64 for exactly 16 bytes), `key_id` (uint32), `width`, `height`
(even), `fps`, `bitrate_kbps` (1–20000), and `expires_at` (RFC3339Nano UTC timestamp,
future and no more than 30 seconds after decoding). Unknown, duplicate,
case-aliased and null fields are rejected. No caller-selected metadata, codec,
input option, executable path or receiver media ports are accepted.

Stdout is an event-only bounded channel. Native stdout/stderr are diverted before
UI startup; only these JSON objects are emitted, with a single terminating newline:

- `{"schema_version":1,"event":"ready","session_id":"..."}` after the real
  Moonlight connection callback, not merely after starting a goroutine.
- `{"schema_version":1,"event":"first_frame","session_id":"..."}` once, after
  a nonempty decoded image has been submitted to the Fyne canvas on its UI
  thread. It is ordered after ready. Submission/Refresh does not establish that
  pixels were presented; actual window/pixel inspection is separate CI evidence.
- `{"schema_version":1,"event":"stopped","session_id":"...","reason":"completed"}`
  or reason `failed`, once after renderer/window cleanup.

Failure before readiness is a failed startup and may exit without a complete
ready/stopped lifecycle. The manager must still stop and join the source child.
The dedicated command clears every inherited `USBRIDGE_*` diagnostic/config
variable at its first main step, before creating the Fyne app or starting native
decoding. This includes frame-dump destinations, skip-decode, decoder overrides
and future prefixed switches. Go's `os.Unsetenv` also updates the C environment in
cgo builds. DISPLAY/XAUTHORITY and non-prefixed runtime settings are preserved. The viewer
accepts DISPLAY only as a literal local `:N` or `:N.S`; hostname/TCP and
protocol-qualified displays are rejected before Fyne startup. CI uses separately
owned capture `:96` and viewer `:97` displays with private Xauthority.
The parent independently filters the prefix before exec, protecting package
initialization too. Stock client diagnostics and settings are unchanged.

No event contains the descriptor, key, URL, or native error text. The parent must
retain its own output bounds, startup timeout and forced-stop fallback.

`NewSourcePreviewService` avoids generating/loading a persistent GameStream
identity. `ConnectToSourcePreview` consumes the descriptor once, reuses the normal
Moonlight player/native teardown, and binds it to the private lease and deadline.
The native start uses the original encrypted URL, fresh key ID in network byte
order, packet size 1056 (pinned client's ANNOUNCE payload 1024), `ENCFLG_ALL`,
`STREAM_CFG_LOCAL`, fixed app version `7.1.431.-1`, and H264 codec support.
The source path skips Go audio-output setup and its native Opus callbacks consume
and discard decoded PCM without opening, writing to or closing a platform audio
backend. Ambient `PULSE_SERVER` cannot direct this silent profile to a local or
remote playback server. The stock audio-output lifecycle is unchanged.
Keyboard, mouse, pen, controllers, raw HID, MIDI, microphone and camera uplinks
are blocked at the native wrapper. Stock HTTP pairing/launch and its packet size,
encryption defaults and key-ID behavior are unchanged.

## Smallest native Linux CI build

Use a complete checkout of the candidate, including its vendored PyroWave files.
Use the repository Go version (1.26.6 here). On an apt-based Linux CI machine:

```sh
sudo apt-get update
sudo apt-get install -y build-essential cmake pkg-config binutils \
  libopus-dev libssl-dev libavcodec-dev libavutil-dev libswscale-dev \
  libpulse-dev libva-dev libvulkan-dev libgl1-mesa-dev xorg-dev
# FFmpeg and Xvfb are runtime acceptance dependencies, separate from the build.
sudo apt-get install -y ffmpeg xvfb xauth

git submodule update --init --recursive client/moonlight-common-c
test "$(git -C client/moonlight-common-c rev-parse HEAD)" = \
  a232e27d5c423eb8e7de8da91f0eefcf9171348f
cd client
go mod download
bash scripts/build_source_preview_viewer.sh
go test -race ./internal/sourcepreview
go test -race ./internal/service -run 'TestSourcePreview|TestDisconnect'
```

The checked-in script builds the reviewed Moonlight submodule through
`scripts/build_moonlight.sh` and the vendored PyroWave archive through
`scripts/build_pyrowave.sh`, then builds only `cmd/source-preview-viewer`.
PyroWave is not used by the H264 preview but its archive is a link dependency of
the existing renderer. Its checked-in provenance pins are:

- PyroWave `509e4f887b585a3f97471fcc804e9de649f2c16f`
- Granite `44362775d36e0c4139352f83efd96bab4e239f66`
- volk `47cddf7ed97b94118a08aacb548a411188e016cc`
- Vulkan headers `015e25c3c91b70eb1a754d36fb14c4ba6ad9b0b9`

Use the vendored, locally patched tree, not fresh upstream replacements.
`build_pyrowave.sh` needs CMake >=3.20, a C++17 compiler and GNU binutils; it
localizes Granite's Vulkan symbols before making `libusbridge-pyrowave.a`.
Fyne and the normal renderer also link GL/X11/Vulkan, even though this small
window uses the CPU-image canvas path. No `usbpass_gousb` build tag is needed.

The service package still compiles `localui`, `onnxruntime_go`, NBD and platform
uplink code. ONNX models and `libonnxruntime.so` are lazy runtime dependencies of
AI Vision/local parsing, which this viewer does not enable. Do not ship fabricated
ONNX placeholders or claim that their presence validates this preview.

## Acceptance still required

A successful build or unit test is not renderer acceptance. Run the actual viewer
against the real source under a controlled Xvfb display, with fresh private launch
keys, an explicitly approved 128x72 test pattern and silent audio.
Observe ready plus first_frame and inspect the actual viewer window/pixels. Check
UI Stop, window close, stdin EOF, source crash, canceled setup and descriptor
expiry; verify both children exit, ports close, no source capture process remains,
and a second fresh launch succeeds. Test incompatible descriptors and altered
component hashes fail closed. Keep source captures confined to the approved
synthetic display and never publish credentials or frame data in logs.


### Inherited-diagnostic capture fixture

`tests/source_preview_no_dumps.py /absolute/path/to/source-preview-viewer` is a
native fixture consumer, not a renderer mock. Its supervisor must start a real
source against the approved synthetic Xvfb display, then supply a fresh private
one-line descriptor on the script's stdin, with the default 128x72 profile. The
supervisor retains and finally stops/joins the source process; keys must not be
passed in argv, environment variables or persisted files.

The fixture deliberately gives the viewer `USBRIDGE_FRAME_DUMP_DIR`,
`USBRIDGE_FRAME_DUMP_EVERY_N=1`, `USBRIDGE_PYROWAVE_DUMP`,
`USBRIDGE_SKIP_DECODE=1`, decoder/log overrides and an unknown future prefixed
switch. It requires ready and a nonempty decoded-image submission, leaves the
real stream running for another second, closes the private stdin lease, requires
completed teardown, and verifies both the frame directory and PyroWave dump path
remain empty. It also points `PULSE_SERVER` at a counted, loopback-only connection
trap and requires zero audio-output connection attempts, draining the listener
before its final assertion. SKIP_DECODE must not prevent first_frame. The only report is a
sanitized pass/fail summary; neither launch credentials nor protocol input are
printed. A pass explicitly says `presented_pixels_verified:false`; inspect the
actual viewer window separately.

Portable scrub/event tests do not need native libraries:

```sh
go test -race ./cmd/source-preview-viewer/environment.go \
  ./cmd/source-preview-viewer/environment_test.go \
  ./cmd/source-preview-viewer/events.go ./cmd/source-preview-viewer/events_test.go
python3 -m py_compile tests/source_preview_no_dumps.py
```

The native fixture must be exercised on the CI machine with the real source and
viewer build; portable tests and Python syntax checks are not substitute evidence.


### Native silent-audio callback check

After building the pinned Moonlight core, compile and run the callback-level
regression test (no source session/capture or actual audio output is started):

```sh
cc -std=c11 -D_DEFAULT_SOURCE -I moonlight-common-c/src \
  -I moonlight-common-c/enet/include $(pkg-config --cflags opus openssl) \
  tests/source_preview_native_audio.c \
  moonlight-common-c/build/libmoonlight-common-c.a \
  moonlight-common-c/build/enet/libenet.a \
  $(pkg-config --libs opus openssl) -lpthread -lm \
  -o dist/source-preview-native-audio-test
dist/source-preview-native-audio-test
```

This exercises the actual shared audio callbacks and real Opus decoder with
counted output-backend functions. Source policy must yield zero opens/writes/
closes while still decoding PCM. Stock policy must retain one open, write and
close. The live PULSE_SERVER trap in the viewer fixture complements this callback
test and checks the dedicated child integration. Neither test establishes
presented video pixels.
