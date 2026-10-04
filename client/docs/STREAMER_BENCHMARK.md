# Streamer benchmark

Gear menu (Control header) → **Run benchmark**. Compares Sunshine and
USBridge Streamer (`"rustshine"` on the wire and in code -- see
`service.BenchBackendLabel`) on the connected host under identical
conditions. It is available in the native clients (Linux, Windows, macOS,
Android), not in the web client, because it needs the moonlight decode
hook -- see [TODO.md](../TODO.md#streamer-benchmark) for what a browser/
WebRTC benchmark path would need.

## What one run does

For each streamer that is ticked:

1. The agent switches its stream backend (`POST /api/bench/backend`). The
   time this takes is the **switch** time.
2. The client starts a fresh stream and waits for the first decoded frame:
   the **first-frame** time. Both startup numbers are shown separately and
   never enter any other metric.
3. After 1 s of settling, the agent starts the test video fullscreen on the
   host (`POST /api/bench/video/start`): a 30 s, 1080p, native 60 fps cut of
   Blender's "Big Buck Bunny" (CC-BY, about 14 MB, downloaded once into the
   agent's state dir), played back as-is by VLC (preferred), ffplay or mpv.
   Being natively 60 fps, every display refresh is already a genuinely new
   picture -- no fps-conversion or marker-overlay filter chain is needed
   (an earlier 24 fps trailer needed both, and the filter chain itself
   could bottleneck a modest host into a slideshow). If the download fails,
   ffplay's `testsrc2` pattern is used instead. The video always starts
   from its first frame, after the stream is up, so both streamers are
   measured on the same content.
4. The client records every frame for the chosen window (30 s – 5 min),
   with the Net Graph on, and the agent samples the host's load over the
   same window (`POST /api/bench/load/start` / `stop`). Then the video and
   the stream stop, and the next streamer runs.

The setup dialog picks the streamers, the host monitor, the window and the
**codec**: "As in video settings" or H.264 / HEVC / AV1 forced for every
run (`controller.SetBenchmarkCodec`; the saved codec is left untouched and
back in force once the benchmark ends). Each run's Codec row still shows
what the host actually negotiated.

At the end the host is switched back to the streamer it had before, and
the stream is restarted if it was running.

## What is recorded

Per frame, from `dr_submit` (`internal/service/bench_frames.c`):

* the frame number, IDR flag and size;
* the host's own capture timestamp (from the RTP timestamp);
* the host's capture+encode time (`frameHostProcessingLatency`);
* when the frame's first packet arrived, when it was reassembled, and when
  it was handed to the decoder (all on the client clock).

Every 100 ms: RTT and its variance, playout jitter and delay, packet, FEC
and loss counters, and frames presented by the renderer. The same 100 ms
tick also drives the live Net Graph HUD (Control footer's graph toggle),
which shows a streamer/codec/bitrate line on every stream -- not just a
benchmark run -- plus its own GPU line:

* **Streamer/codec/bitrate:** the active backend (pushed once per stream
  start from `GET /api/bench/status`, not polled live), the codec
  moonlight-common-c actually negotiated (`NegotiatedVideoCodecName`), and
  a live bitrate averaged over the last ~1 s from a cumulative
  decoded-bytes counter (`dr_submit`'s `DECODE_UNIT.fullLength`, see
  `net_graph.go`'s `BytesVideo`).
* **GPU:** `GPU <host> - <client>` -- the agent host's GPU (same
  `GET /api/bench/status` push, see `gpu` below) and the client's own
  decode/render GPU (`VkPhysicalDeviceProperties.deviceName` on
  Windows/Linux/Android, the system default Metal device's name on
  macOS/iOS; `--` on the web client and wherever the host side hasn't
  answered yet).

Every 500 ms, on the agent (`agent/internal/hostload`, Windows performance
counters -- the ones Task Manager shows; other host OSes, and any PDH
failure, send no samples and now also name why, see `/api/bench/load/stop`
below): whole-machine CPU and the streamer processes' CPU (`sunshine`,
`usbridge-streamer`; percent of all cores), and GPU utilization per engine
type -- `3d`, `encode`, `decode`, `copy`, and `codec` for a shared
encode+decode block (AMD's "Video Codec") -- for the whole machine and for
the streamer processes alone. Per type, the busiest engine counts, each
engine's load summed over its processes. Stored per run as `host_load`.

## How it is analysed (`internal/service/bench_analysis.go`)

* **Smoothness:** intervals between frames handed to the decoder. From
  these come the average fps, the 1 % low (1000 / the p99 interval), the
  p50/p95/p99/max intervals and their std-dev. A **stall** is an interval of
  at least 3 frame times and at least 50 ms. A **hitch** is over 2 frame
  times but below that threshold.
* **Every stall gets a cause:**
  * *loss*: frame numbers are missing across the gap. The recovery is
    reported as IDR or RFI, depending on whether the next frame was a
    keyframe.
  * *host*: the host's own capture timestamps have the same gap, so the
    host produced nothing.
  * *network*: the host produced frames on time, and they arrived late.
* **Host:** encode time avg/p95/max, capture rate, capture pacing std-dev,
  the longest capture gap, bitrate, and the number of keyframes.
* **Host load:** averages of the samples above -- GPU 3D / video encode /
  video decode for the streamer and for the whole host, and CPU for both
  (`codec` counts as encode and decode).
* **Network:** RTT, per-frame network jitter |Δarrival − Δcapture|, frame
  transfer time (first packet → reassembled), packet loss including
  FEC-repaired packets, and FEC failures.
* **Recovery:** loss events, frames lost, recovery time avg/max, and the
  IDR vs RFI count.

A streamer that leaves the capture timestamps at zero gets "n/a" for the
host-cadence and network-jitter rows. Its stalls are then classified only
as loss or network.

## Results

The results dialog has a comparison table (the best value is in green,
plus a Codec row from each run's own `NegotiatedVideoCodecName`), frame-time
timelines per streamer with every stall marked in its cause's color,
overlaid fps / host encode time / network jitter / RTT charts, the
streamer's GPU load (3D, encode, decode) and CPU load charts, startup
bars, and a list of every stall. `results.json` (all raw frames and ticks)
and `chart.png` are auto-saved to
`<user config dir>/usbridge-client/benchmarks/<timestamp>/`; the dialog's
**Save results…** button additionally copies both files to a folder of the
operator's choosing.

## Agent endpoints

| Endpoint | |
|---|---|
| `GET /api/bench/status` | active backend, backends available to switch to, player, `gpu` (host GPU name, `account.CollectDeviceInfo`) |
| `POST /api/bench/backend` `{"kind":"sunshine"\|"rustshine"}` | switch, returns `switch_ms` |
| `POST /api/bench/prepare` | download the test video |
| `POST /api/bench/video/start` / `stop` | play / close the test video |
| `POST /api/bench/load/start` / `stop` | sample host CPU/GPU load; `stop` returns `{"samples":[...]}`, plus `"error"` naming why whenever `samples` is empty (`hostload.Sampler.LastError`) |

The player stops on its own after 15 minutes if no client stops it. On
Windows it is launched into the interactive session (a Session 0 window
would be invisible to capture). `USBRIDGE_BENCH_VIDEO=/path/file` uses a
local video instead of the downloaded clip.
