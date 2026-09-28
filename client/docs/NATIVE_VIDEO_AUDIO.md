# Native Hardware Video & Audio (Moonlight Mode)

Moonlight streaming uses platform-native hardware APIs for zero-subprocess,
zero-pipe video decode and audio output on every platform, relying solely on hardware acceleration via Vulkan and Metal.

## Architecture

```
Sunshine (server) → H.264 RTP → moonlight-common-c
                                        │
                                  dr_submit (C callback)
                                        │
                          ┌─────────────▼────────────────┐
                          │   platform_dr_submit (CGO)   │
                          └─────────────┬────────────────┘
                                        │ GPU texture
                                  goVTFrame (Go callback)
                                        │
                             vtFrameCallback (Go func)
                                        │
                           Vulkan / Metal Hardware Context

Opus packets → ar_decode (C) → platform_ar_decode (CGO) → native audio API
```

## Per-platform implementation

| Platform | Video decoder & Rendering | Audio output | File |
|----------|--------------|--------------|------|
| macOS | Metal + VideoToolbox `VTDecompressionSession` | CoreAudio `AudioQueue` | `moonlight_cgo_apple.go` |
| iOS | Metal + VideoToolbox | CoreAudio `AudioQueue` | `moonlight_cgo_apple.go` |
| Linux | Vulkan + libavcodec (native `h264`/`hevc`/`av1` + VA-API or NVDEC hwaccel, or `*_qsv`) — see [Linux decode & render paths](#linux-decode--render-paths) | PulseAudio `pa_simple` | `moonlight_cgo_linux.go` |
| Windows | Vulkan + libavcodec `h264_d3d11va` | WASAPI `IAudioRenderClient` | `moonlight_cgo_windows.go` |
| Android | Vulkan + `AMediaCodec` NDK | `AAudio` NDK (low-latency) | `moonlight_cgo_android.go` |

## C interface (moonlight_cgo_shared.h)

Each platform CGO file implements these four functions:

```c
void platform_ar_init(int channels, int sample_rate);   // open audio device
void platform_ar_cleanup(void);                          // close audio device
void platform_ar_decode(const opus_int16 *pcm,
                        int byte_count, int samples);    // play decoded PCM
int  platform_dr_submit(PDECODE_UNIT du);                // decode H.264 frame
void platform_post_stop(void);                           // teardown after stream stop
```

The shared header provides: opus decoder, connection callbacks, `do_li_start/stop`, input forwarders.

## Linux decode & render paths

**Decoder selection** (`linux_av_init` in `moonlight_cgo_linux.go`), first that works wins:

1. **VA-API** (Intel/AMD): native `h264`/`hevc`/`av1` decoder + `AV_HWDEVICE_TYPE_VAAPI` hw_device_ctx.
2. **NVDEC** (NVIDIA): native decoder + `AV_HWDEVICE_TYPE_CUDA` hw_device_ctx.
3. **QSV** (Intel oneVPL): the separate `h264_qsv`/`hevc_qsv`/`av1_qsv` decoders.
4. **Software**: native decoder, slice threading (no frame threading — it adds latency).

VA-API and NVDEC are *hwaccels of the native decoder*, not separately named
decoders — there is no `h264_vaapi`/`hevc_nvdec` decoder in FFmpeg. (Up to
3.0.36 the code looked those names up, so every non-Intel GPU silently fell
back to single-threaded software decode: ~30 ms per 2560x1600 HEVC frame on
the RTP receive thread, capping the stream at ~20-30 fps and stalling socket
draining — the "choppy compared to stock Moonlight" bug.) A hw decoder that
opens but yields no frame within its first 20 packets is blacklisted for the
process and the next candidate is tried on a fresh IDR.

**Render paths** (`vk_video_impl_linux.c`), per frame:

| Path | Taken for | CPU work per frame |
|------|-----------|--------------------|
| dma-buf zero-copy | VA-API / QSV surfaces exported as NV12 dma-buf | none (import + GPU YCbCr sample) |
| NV12 upload | NVDEC (CUDA) frames, software decode | hw download (NVDEC) or yuv420p→NV12 interleave (sw) + one 12 bpp memcpy; YCbCr→RGB, scaling and letterbox on the GPU via `VkSamplerYcbcrConversion` |
| RGBA blit | fallback (no Vulkan 1.3 / ycbcr support, 10-bit P010) | `sws_scale` to RGBA + R/B swizzle on the CPU |

Measured on an RTX 2080 Ti, HEVC 2560x1600@60: software decode ~20-30 ms/frame;
NVDEC + RGBA blit ~16 ms/frame of CPU output work (49 fps); NVDEC + NV12 upload
~2.3 ms decode + ~3.2 ms download/submit — steady 60 fps.

Diagnostics env vars:

| Var | Effect |
|-----|--------|
| `USBRIDGE_HWDEC=vaapi\|cuda\|qsv\|sw` | Force one decoder candidate (`sw` = software) |
| `USBRIDGE_VK_NO_NV12=1` | Disable the NV12 upload path (falls back to RGBA blit) |
| `USBRIDGE_PLAYOUT_BUFFER=0` | moonlight-common-c: bypass the adaptive playout delay (release frames immediately, like upstream) |
| `USBRIDGE_LOG_FRAME_JITTER=1` | Log network frame-arrival cadence every 60 frames |

Check `app.log` for `libavcodec: using <decoder> via <vaapi|cuda|qsv>` and
`first NV12 frame -> Vulkan GPU YCbCr` / `first zero-copy frame` to see which
paths a session actually took.

## Build dependencies

### macOS / iOS
No external dependencies — Metal, VideoToolbox and CoreAudio are Apple system frameworks.
```
-framework Metal -framework VideoToolbox -framework CoreMedia -framework CoreFoundation
-framework CoreVideo -framework AudioToolbox
```

### Linux
Install at **build time** (headers) and **run time** (shared libs):
```bash
# Build deps
sudo apt-get install -y libavcodec-dev libavutil-dev libswscale-dev libasound2-dev libvulkan-dev

# Runtime (target machine)
sudo apt-get install -y libavcodec60 libavutil58 libswscale7 libasound2 libvulkan1

# Optional hardware acceleration
sudo apt-get install -y libva2 libva-drm2        # Intel/AMD VA-API
# NVIDIA: install nvidia-driver with CUDA support
```

### Windows
FFmpeg MinGW shared build (avcodec, avutil, swscale) + system WASAPI and Vulkan drivers.

Download FFmpeg: https://github.com/BtbN/FFmpeg-Builds/releases  
Pick: `ffmpeg-master-latest-win64-gpl-shared.zip`

```bash
export FFMPEG_ROOT=/path/to/ffmpeg-win64-gpl-shared
scripts/build_windows.sh
```
The build script copies `avcodec-*.dll`, `avutil-*.dll`, `swscale-*.dll` into `dist/windows/`.

### Android
`AMediaCodec` and `AAudio` are part of the Android NDK. Vulkan API is loaded dynamically.  
Link flags: `-lmediandk -laaudio -landroid -lvulkan`

Note: moonlight-common-c must be compiled for Android ARM64 (NDK toolchain).

## AI Vision live overlay

The "AI Vision" checkbox (video start dialog) reuses the local ui.parse
ONNX pipeline (see the client README's "Local ui.parse Offload" section)
against the live video feed instead of a static screenshot. How the
resulting detection boxes reach the screen depends on whether a
platform's decode path above ever produces a CPU-writable frame:

| Platform | Video path | Overlay mechanism |
|----------|-----------|--------------------|
| Linux, dma-buf / NV12 paths (common case) | GPU YCbCr sampling, no per-frame RGBA buffer | Occasional CPU sample (gated by `goAIVisionShouldSample`) feeds the detector; boxes and the Net Graph HUD are drawn by the Vulkan overlay layer (`vk_overlay_common.h`) |
| Linux, RGBA blit fallback | Vulkan (CPU RGBA uploaded to a texture each frame) | Drawn in place into the RGBA buffer before `vk_video_try_submit` (`ai_vision.go`'s `ApplyAIVisionOverlay`) |
| macOS, CPU-fallback decode | Only taken when `metal_video_try_submit` declines | Same in-place RGBA drawing, in `vt_callback`'s fallback branch |
| macOS, Metal fast path (common case) | Zero-copy IOSurface → `CVMetalTextureCache`, no CPU pixel access ever | A second transparent `CALayer` (`g_overlay_layer` in `metal_video_impl_darwin.m`) stacked above the video IOSurface layer, updated only once per completed detection pass (~every 2s) — Core Animation composites it on the GPU for free every frame in between. Kicking off a fresh detection pass costs one occasional CPU readback of the `CVPixelBufferRef` (gated by `goAIVisionShouldSample`), not a per-frame cost. |
| Android / Windows (Vulkan `AHardwareBuffer` zero-copy) | Zero-copy, no CPU pixel access | Not wired up yet — would need the same native-compositor-layer approach as macOS's Metal path (see `VulkanOverlayBridge.kt`'s cursor overlay for the existing Android pattern to extend) |
| iOS | Metal, same zero-copy shape as macOS | Not wired up yet |

## Performance

| Metric | New Native Pipeline (Vulkan/Metal) |
|--------|------------------------------|
| Processes | 0 extra processes |
| OS pipes | 0 |
| IPC latency | 0 ms |
| Video decode | Hardware GPU always |
| Audio | Native API (CoreAudio / ALSA / WASAPI / AAudio) |
| Startup time | ~50 ms (codec open) |
| CPU video (1080p30) | <2% (GPU hardware) |
