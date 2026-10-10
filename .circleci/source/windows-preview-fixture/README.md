# Synthetic Windows preview encoder fixture

This CI-only executable recognizes two exact encoder invocations from the
unchanged public source snapshot
`2e07af3484369bc68bc8091d5a04969867eff0f9`. It substitutes owned synthetic media
for those roles. **Desktop capture is structurally unavailable.** The executable
contains no encoder, subprocess launch, desktop API, network client, shell,
environment configuration, or general-purpose argument forwarding.

It must never appear in the normal agent dialog or production encoder selection.
Receipts must identify `synthetic-substitution-only` and explicitly retain
`desktop_capture_tested: false`. This fixture can establish the real source,
transport and viewer path with synthetic inputs; it cannot establish capture,
Windows viewer rendering, or production dialog acceptance by itself.

## Frozen input contracts

`main.go` contains the complete, ordered vectors copied from the existing public
archive, not a current/private repository:

- `source/media/windows_capture.go`: live 128×72, 30 fps, yuv420p, lossless all-IDR
  H264, primary rectangle at 0,0, cursor disabled. The strings `gdigrab` and
  `desktop` are recognition-only data, never inputs to an executable or API.
- `source/audio/pcm.go`: private stdin s16le, 48 kHz, stereo → 128 kbps CBR,
  low-delay, 5 ms Ogg Opus, one data packet per page.

Any omission, extra argument, different value/order/profile/input/output, help,
version probe, or unknown invocation exits 2 with no stdout. There are no
fixture-specific runtime flags.

Reviewed archive SHA256:
`7a8ec9b04f0b3f090de55dc6fc7194e7bd7caf93a08a11182c7c03dcfce1ab2b`.

## Build and assets

Use the repository-pinned Go 1.26.9 and a trusted, officially distributed FFmpeg
with libx264 and libopus. The CI owner must provision and record FFmpeg package,
binary and dependency provenance. The generator does not download software.
FFmpeg is used **only at build/verification time**, with explicit synthetic
`lavfi color` and `lavfi anullsrc` inputs; it is never needed at fixture runtime.

From this directory, in the Windows MSYS2 build shell:

```sh
python build_assets.py --ffmpeg /absolute/path/to/ffmpeg.exe \
  --output /absolute/path/to/private-package
CGO_ENABLED=0 go build -trimpath \
  -ldflags "$(cat /absolute/path/to/private-package/fixture-ldflags.txt)" \
  -o /absolute/path/to/private-package/ffmpeg-fixture.exe .
go test ./...
python verify_fixture.py --ffmpeg /absolute/path/to/ffmpeg.exe \
  --fixture /absolute/path/to/private-package/ffmpeg-fixture.exe
```

Use paths that are absolute according to the Python interpreter actually running
on Windows; when invoking native UCRT64 Python from MSYS, pass converted Windows
paths if necessary. The Go module has no third-party dependencies.

The generator produces:

| Adjacent asset | Media | Build pin |
| --- | --- | --- |
| `fixture-blue.h264` | One independent AUD/SPS/PPS/[SEI]/IDR frame, color `0x164cb4` | `main.videoBlueSHA256` |
| `fixture-orange.h264` | Same profile, color `0xdc6432` | `main.videoOrangeSHA256` |
| `fixture-silence.ogg` | Complete 29.995 s silent stereo Opus stream | `main.audioSilenceSHA256` |

`fixture-ldflags.txt` supplies all three `-X main.<pin>=<sha256>` assignments.
`fixture-assets.json` records the hashes, sizes, decoded RGB centers/YUV frame
hashes, generator version/hash and explicit synthetic-substitution label. Ogg
stream serials can differ between generations, so build from the flags produced
with that exact asset set. Do not reuse old flags against new assets.

The runtime only uses compiled pins and those three fixed files adjacent to its
executable. It does not read the JSON/flags, current directory, environment or
any caller-selected path. It validates **all three assets before any media
output**, rejecting unset/malformed pins, missing/nonregular/symlink files,
files above 64 KiB per video frame or 1 MiB for Ogg, changed file identities/sizes,
and wrong SHA256 values. It then verifies Annex-B access-unit boundaries and
Ogg CRCs, serial/sequence numbers, headers, 5 ms stereo CBR packet layout,
granules, page count and EOS. Generation strictly decodes the whole repeated
900-frame video stream and the whole Opus stream before writing assets.

These outputs and compiled executables are generated CI inputs, not source
artifacts. Do not commit or publicly upload them. Preserve the existing source
artifact audience hold and receipts-only publication policy.

## Runtime timing and lifecycle

- Video emits 900 frames, paced at 30 fps. Blue is first; colors alternate every
  30 frames. Each frame is independently decodable. FFmpeg 7.1.5 locally decoded
  the owned colors to RGB `[22,75,180]` and `[219,99,49]`; the generated manifest
  records the actual generator's results instead of assuming exact RGB rounding.
- Audio emits both Ogg headers immediately, then 6000 data pages at 5 ms cadence.
  It reads/discards at most 5,759,040 bytes of matching private PCM, 960 bytes per
  ordinary page, without saving or echoing PCM. A short input fails closed.
- 29.995 s is intentional: low-delay Opus adds a final padded packet. A 30.000 s
  input produces 6001 packets, whose complete 5 ms replay would reach the
  30-second deadline. This profile produces 6000 packets and includes EOS.
- The hard **30-second media lease starts after bounded asset verification**.
  Its watchdog terminates even a blocked stdin/stdout operation. Expected source
  cancellation normally stops the fixture earlier. There are no child processes
  and therefore no wrapper-owned grandchild or nested-FFmpeg Job Object problem.
- On an observed broken output write, it exits immediately. Generic stderr is
  limited to `synthetic fixture failed`; stdout contains media only. Deadline
  expiration has no diagnostic. Source/harness process cancellation remains the
  authoritative cleanup mechanism; no runtime signal API or environment hook
  is offered.

The hosted harness must still contain its real source/viewer process tree and
verify cleanup. The fixture's no-descendant design does not waive those gates.

## Checks

`go test ./...` exercises every argument deletion/modification/insertion,
fixed contract drift, asset fail-closed behavior including symlinks where the
platform permits unprivileged creation, malformed Annex-B and Ogg/CRC streams,
all 900/6000 scheduled outputs, exact bounded PCM consumption, short PCM,
wall-clock waits, cancellation and real subprocess broken-pipe behavior. A
runtime source/import allowlist prevents quietly adding process/capture/network
or environment-selection extension points. Tests may launch helper children;
those test-only imports are not part of the fixture executable.

`verify_fixture.py` runs the actual pinned fixture for both roles concurrently,
checks output byte-for-byte and wall-clock cadence, and strictly decodes the
**actual emitted complete streams**. It reports a small JSON verification
receipt and never marks desktop capture tested. Run this on Windows for native
fixture evidence; a Linux run and a Windows cross-build are not native Windows
execution or viewer acceptance.
