# Windows decode pipeline: threading, decoder threads, jitter buffer

Investigation of October 2026 (client 3.0.88). Windows client on a laptop with
an AMD Radeon 780M iGPU (Vulkan Video decode, zero-copy render), streaming 4K
HEVC from a Linux USBridge agent.

## Symptom

- The macOS client streams the same agent at a smooth 120 fps.
- The Windows client streamed at about 60 fps, and input lag grew to 1-2 seconds.
- Against other streamers the Windows client only showed a lower frame rate,
  without the growing lag.

## Findings from `app.log`

1. **Decode ran on the network receive thread.** The client set
   `CAPABILITY_DIRECT_SUBMIT`, so `avcodec_send_packet`, `avcodec_receive_frame`
   and frame delivery all ran inside moonlight-common-c's RTP receive loop. The
   log showed `SLOW video receive-loop iteration: ~9000us not draining socket`
   on every frame. At 120 fps the budget is 8.33 ms.
2. **The playout buffer slept on that same thread.** Our moonlight-common-c
   fork's playout (jitter) buffer added 7-50 ms per frame. On the
   direct-submit path that sleep also stops the socket from being drained.
3. **The YCbCr descriptor pool was too small.** It held one descriptor per
   cached image. A YCbCr-converted combined image sampler can use several
   descriptors per set. On AMD, `combinedImageSamplerDescriptorCount` is 3
   for 2-plane formats. The pool ran out after a few of ffmpeg's surfaces, and
   frames decoded into any other surface were never drawn. The log showed
   `vkAllocateDescriptorSets failed` 4,915 times in one session.
4. **The renderer called `vkQueueWaitIdle(decode queue)` before each frame.**
   That waits for every newer decode ffmpeg has queued, not only the frame
   being drawn. It also touched a queue that ffmpeg submits to from another
   thread, without ffmpeg's queue lock.
5. The display runs at 60 Hz with FIFO_RELAXED, so on-screen frame rate is
   capped at 60. That part is expected. The lag is not.

### Why the lag grows to seconds

When decode costs more than one frame interval, the receive thread falls
behind. Unread packets queue in the kernel socket buffer, which is 2.4 MB
(`Actual receive buffer size: 2424832`). At an 8 Mbps stream that is about
2.4 seconds of video. The lag therefore grows until the buffer overflows, and
then a loss and IDR cycle resets it. The official clients avoid this: they
decode off the network thread from a bounded 15-frame queue. If that queue
overflows, they flush it and request an IDR frame, so the backlog never grows
past a few frames.

## Official Moonlight comparison

Sources: moonlight-qt `de2467e` (2026-10-03) and moonlight-common-c `f900dd4`
(2026-09-26), cloned fresh for this investigation.

| | moonlight-qt (Windows) | USBridge 3.0.88 (Windows) |
|---|---|---|
| Capability | `CAPABILITY_PULL_RENDERER` (`ffmpeg.cpp:153`) | `CAPABILITY_DIRECT_SUBMIT` |
| Decode thread | own `FFDecoder` thread, `LiWaitForNextVideoFrame` / `LiPollNextVideoFrame` | RTP receive thread |
| Backpressure | bounded 15-frame queue, flushed with an IDR request on overflow | kernel socket buffer, seconds deep |
| Decoder flags | `AV_CODEC_FLAG_LOW_DELAY` | none |
| Hardware decoder | D3D11VA (default renderer) | Vulkan Video, zero-copy |
| Jitter buffer | none | fork's playout buffer, always on |

## Offline benchmark

`tools/decode_bench/decode_bench.c` uses the same threading logic as the
client, with real Vulkan Video decode on the same GPU:

- A "network" thread releases one access unit every 1/fps seconds.
- The three modes are `direct`, `thread` (moonlight-common-c's `VideoDec`
  thread) and `pull` (the moonlight-qt loop).
- Latency is measured from the scheduled packet arrival until the frame's
  timeline semaphore signals, meaning GPU decode is done and the frame is
  ready to render.
- The clips are 4K HEVC, P-frames only with an infinite GOP, encoded by NVENC
  at 8 and 40 Mbps.

### 1. Threading mode alone (one decoder thread, `LOW_DELAY`)

4K @ 120 fps, 10 s:

| clip | mode | frames decoded | latency p50 | latency p99 | max time the network thread is blocked | CPU (% of one core) |
|---|---|---|---|---|---|---|
| 8 Mbps | direct | 1199/1199 | **507 ms** | **1021 ms** | 10.3 ms | 9.6 |
| 8 Mbps | thread | 173 (queue overflow, then no IDR in a file) | 66 ms | 140 ms | 0.05 ms | 36.7 |
| 8 Mbps | pull | 176 (same) | 68 ms | 141 ms | 0.01 ms | 25.8 |
| 40 Mbps | direct | 1199/1199 | **698 ms** | **1370 ms** | 10.4 ms | 10.8 |
| 40 Mbps | thread | 122 (same) | 70 ms | 141 ms | 0.48 ms | 35.5 |
| 40 Mbps | pull | 120 (same) | 68 ms | 141 ms | 0.02 ms | 29.4 |

4K @ 60 fps: all three modes are the same. Latency p50 is 9.3-9.8 ms and
p99 is 10.1-10.8 ms, with no overflows. CPU is 15-21% of one core.

`direct` reproduces the field bug: lag climbs steadily, to about 1 s in 10 s.
In `thread` and `pull` the network thread is never blocked, but the decoder
still cannot keep up, so the bounded queue overflows. In a live stream an
overflow becomes an IDR request instead of seconds of lag.

### 2. The real limit: ffmpeg's Vulkan decoder with one thread

`ffmpeg -hwaccel vulkan -threads N` on the 40 Mbps clip, 780M:

| decoder threads | Vulkan decode throughput |
|---|---|
| 1 | **104 fps** (9.6 ms/frame, strictly one frame in flight) |
| 2 | 202 fps |
| auto | 216 fps |

D3D11VA decodes at 263 fps with `-threads auto`. With one decoder thread, a
4K@120 stream can never keep up on this GPU, whatever thread calls into
libavcodec. Frame threading requires `AV_CODEC_FLAG_LOW_DELAY` to be off,
because libavcodec disables frame threads when it is set.

### 3. Two frame threads, steady state (after the first 0.5 s)

4K @ 120 fps:

| clip | mode / threads | latency p50 | latency p99 | CPU |
|---|---|---|---|---|
| 8 Mbps | direct / 1 | 541 ms | 1029 ms | 11.7 |
| 8 Mbps | direct / 2 | 50 ms | 157 ms | 18.5 |
| 8 Mbps | thread / 2 | 34 ms | 144 ms | 31.0 |
| 8 Mbps | **pull / 2** | **18 ms** | **79 ms** | 33.7 |
| 8 Mbps | pull / 3 | 19 ms | 69 ms | 37.5 |
| 40 Mbps | direct / 1 | 743 ms | 1394 ms | 10.2 |
| 40 Mbps | direct / 2 | 11 ms | 49 ms | 27.8 |
| 40 Mbps | thread / 2 | 13 ms | 70 ms | 32.0 |
| 40 Mbps | **pull / 2** | **11 ms** | **48 ms** | 35.1 |
| 40 Mbps | pull / 3 | 18 ms | 66 ms | 26.6 |

The p99 tail comes from short episodes where the shared iGPU decodes below
120 fps. Latency then climbs by about 1.2 ms per frame and drains back down.
This is GPU clock and load behavior, not threading. In these episodes `pull`
recovers best.

At 4K @ 60 fps, 2 threads cost exactly one frame of latency:

| mode / threads | latency p50 | latency p99 |
|---|---|---|
| pull / 1 | 9.4-9.7 ms | 10.3-10.7 ms |
| pull / 2 | 16.8 ms | 17.1 ms |

## What changed

- **Decode mode** (`moonlight_cgo_windows.go`, env `USBRIDGE_DECODE_MODE`):
  - `pull` is the default. It uses `CAPABILITY_PULL_RENDERER` and its own
    decode thread running moonlight-qt's loop.
  - `thread` and `direct` remain available for A/B testing.
- **Decoder threads** (`win_vk_decode_threads`, env `USBRIDGE_DECODE_THREADS`):
  - 2 frame threads when width × height × fps is above 4K@60. Example: 4K@120.
  - 1 thread with `LOW_DELAY` otherwise. Examples: 4K@60, 1440p@120.
  - The 4K@60 cutoff was measured on the 780M. A faster GPU would manage with
    1 thread and save a frame of latency, so this heuristic may need a
    per-GPU check later.
- **Jitter buffer** is a "Jitter Buffer" checkbox in the video settings
  dialog. It is Windows only and off by default
  (`service.SetPlayoutBufferEnabled`).
  - The fork now re-reads `USBRIDGE_PLAYOUT_BUFFER` at every stream start.
  - The fork now honors the flag on the queued and pull path too. Before, the
    off switch worked only on the direct-submit path.
- **Renderer** (`vk_video_impl_windows.c`):
  - The YCbCr descriptor pool is sized at `cache × 3`.
  - The image-view cache grew from 8 to 32 entries.
  - Eviction is round-robin, and it waits for the in-flight fence before
    rewriting a descriptor set.
  - Each frame now waits on its own `AVVkFrame` timeline semaphore value
    instead of `vkQueueWaitIdle(decode queue)`.
- **Smooth Motion** (frame-smoothing concealment) is hidden from the video
  settings dialog. It stays off. In live use it made the picture worse and
  gave no visible benefit.

## Reproducing

```sh
# MSYS2 UCRT64 shell
gcc -O2 tools/decode_bench/decode_bench.c -o decode_bench.exe \
    $(pkg-config --cflags --libs libavformat libavcodec libavutil) -lvulkan-1 -lwinmm
ffmpeg -f lavfi -i "testsrc2=size=3840x2160:rate=120,noise=alls=12:allf=t" -t 12 \
    -c:v hevc_nvenc -preset p1 -tune ull -rc cbr -b:v 40M -bf 0 -g 100000 -f hevc clip_40m.hevc
./decode_bench.exe clip_40m.hevc pull 120 10
BENCH_THREADS=2 BENCH_NOLOWDELAY=1 ./decode_bench.exe clip_40m.hevc pull 120 10
```

In a live session, the client logs its configuration at stream start:

- `libavcodec/win: decode mode = …, playout buffer …`
- `libavcodec/win: using hevc (… N decode threads, WxH@FPS)`

## Live result (same laptop, Linux USBridge agent, 4K@120 HEVC, 18 Mbps)

- The stream starts and runs at 110-125 fps decoded. The display stays at
  60, because of the 60 Hz panel and vsync.
- `SLOW video receive-loop iteration` and `vkAllocateDescriptorSets failed`
  are gone. The lag no longer builds up.
- Two startup fixes came out of live testing:
  - The decoder is now created in `dr_setup` rather than on the first frame.
    Vulkan Video session setup takes about 1 s and overflowed the queue.
  - The GUI's first frame no longer goes through a 4K CPU readback, which
    took about 270 ms. A nil-pixels frame is enough for the video widget to
    create the Vulkan overlay. CPU readback now runs on its own thread, and
    only for AI Vision, or as a fallback if the overlay is not up after 2 s.
- `avcodec_send_packet` returning `EAGAIN` used to count as accepted, which
  silently dropped the packet. It is now drained and retried.
- If no frame is decoded for 1 s, a `decode stall:` line is logged. It
  records the decode thread's stage, the in/out counters, the ffmpeg error
  codes and the RTP packet and FEC counters.

### Open issue

The decoder thread count comes from the *requested* fps. If a session
requests 60 fps but the host sends more (about 107 fps was seen), a 4K
stream gets one decoder thread. That cannot keep up, so the queue overflows
and requests an IDR about once a second. Choosing the thread count from the
measured arrival rate would fix this.
