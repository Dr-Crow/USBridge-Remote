# Streamer benchmark

Gear menu (Control header) → **Run benchmark**. Compares Sunshine and
RustShine on the connected host under identical conditions. It is available
in the native clients (Linux, Windows, macOS, Android), not in the web
client, because it needs the moonlight decode hook.

## What one run does

For each streamer that is ticked:

1. The agent switches its stream backend (`POST /api/bench/backend`). The
   time this takes is the **switch** time.
2. The client starts a fresh stream and waits for the first decoded frame:
   the **first-frame** time. Both startup numbers are shown separately and
   never enter any other metric.
3. After 1 s of settling, the agent starts the test video fullscreen on the
   host (`POST /api/bench/video/start`): the Blender "Sintel" trailer
   (CC-BY, fast cuts, about 15 MB, downloaded once into the agent's state
   dir) played by ffplay with a white marker sliding along the bottom at
   60 fps. Because of the marker every display refresh is a genuinely new
   picture, even though the trailer itself is 24 fps. mpv or VLC are used
   when ffplay is missing, without the marker. If the download fails,
   ffplay's `testsrc2` pattern is used instead. The video always starts
   from its first frame, after the stream is up, so both streamers are
   measured on the same content.
4. The client records every frame for the chosen window (30 s – 5 min),
   with the Net Graph on. Then the video and the stream stop, and the next
   streamer runs.

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
and loss counters, and frames presented by the renderer.

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
* **Network:** RTT, per-frame network jitter |Δarrival − Δcapture|, frame
  transfer time (first packet → reassembled), packet loss including
  FEC-repaired packets, and FEC failures.
* **Recovery:** loss events, frames lost, recovery time avg/max, and the
  IDR vs RFI count.

A streamer that leaves the capture timestamps at zero gets "n/a" for the
host-cadence and network-jitter rows. Its stalls are then classified only
as loss or network.

## Results

The results dialog has a comparison table (the best value is in green),
frame-time timelines per streamer with every stall marked in its cause's
color, overlaid fps / host encode time / network jitter / RTT charts,
startup bars, and a list of every stall. `results.json` (all raw frames and
ticks) and `chart.png` are saved to
`<user config dir>/usbridge-client/benchmarks/<timestamp>/`.

## Agent endpoints

| Endpoint | |
|---|---|
| `GET /api/bench/status` | active backend, backends available to switch to, player |
| `POST /api/bench/backend` `{"kind":"sunshine"\|"rustshine"}` | switch, returns `switch_ms` |
| `POST /api/bench/prepare` | download the test video |
| `POST /api/bench/video/start` / `stop` | play / close the test video |

The player stops on its own after 15 minutes if no client stops it. On
Windows it is launched into the interactive session (a Session 0 window
would be invisible to capture). `USBRIDGE_BENCH_VIDEO=/path/file` uses a
local video instead of the trailer.
