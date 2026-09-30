# HDR and 4:4:4 color

The video dialog has two Pro color options for H.265 streams from a RustShine
host: **4:4:4 chroma** (full-resolution color, sharper text) and **HDR**
(HEVC Main10, BT.2020 + PQ). They are independent. Encoding happens on the
host, so availability depends on the host's GPU, the license, and whether
this client can display the result. The streamer-side details, including how
each mode was verified, are in rust-shine's `docs/COLOR_MODES.md`.

## When the options are enabled

A row is enabled only when all of these hold:

1. H.265 is selected (both are HEVC profiles).
2. The agent reports it available (`color_444_available` / `hdr_available`
   from the streamer's `/api/status`, passed through unchanged by the agent).
   That means the host hardware can encode it **and** the license is Pro or
   Enterprise.
3. HDR only: this client can display HDR (`service.HdrDisplaySupported`). If
   the host offers HDR but this client can't show it, the row says so instead
   of the "requires Pro" hint.

## Host support (what the agent reports)

| Host | GPU / encoder | 4:4:4 | HDR |
|---|---|---|---|
| Linux | NVIDIA (NVENC) | yes | yes |
| Linux | Intel / AMD (VAAPI) | yes | no (not implemented in the streamer) |
| Windows | NVIDIA (NVENC) | yes | yes |
| Windows | Intel (QSV) | yes | yes |
| Windows | AMD (AMF) | no (AMF has no 4:4:4 profile) | written, not verified on AMD hardware |
| macOS | Apple Silicon (VideoToolbox) | no (no 4:4:4 encode in VideoToolbox) | yes |
| SBC (Rockchip / Allwinner) | hardware encoder | no | no |

Choosing HDR and 4:4:4 together negotiates HDR only: the host doesn't
advertise the combined 10-bit 4:4:4 mode yet.

On a Linux host whose desktop is itself in HDR mode (KDE/GNOME HDR on), an
HDR stream carries the desktop's HDR picture as-is. An SDR stream from that
same desktop is tonemapped back to SDR on the host, so it doesn't look grey.

## Client support (what this app can show)

| Client | 4:4:4 | HDR |
|---|---|---|
| macOS | decodes (not re-checked in this pass) | **yes**: VideoToolbox decodes to 10-bit, Core Animation shows it with EDR |
| Linux | decodes via the CPU fallback (`sws_scale` to RGBA), slower than the GPU NV12 path | no: the option is disabled (8-bit render path, no PQ handling) |
| Windows | decodes | no: the option is disabled (8-bit Vulkan swapchain) |
| Android / iOS / Web | not checked | no: the option is disabled |

## Host load (Linux NVIDIA, RTX 2080 Ti, 3840x2160 at 60 fps)

Measured on the host with the streamer's own capture/encode loop, 2026-09-30.
Latency is capture to encoded frame. CPU is the streamer process, where 100%
is one core. ENC is the NVENC unit's utilization.

| Mode | Latency avg / p99 | CPU | NVENC | GPU power |
|---|---|---|---|---|
| SDR 4:2:0 | 8.1 / 8.3 ms | 5% | 40% | 76 W |
| HDR 4:2:0 | 7.9 / 8.1 ms | 5% | 40% | ~79 W |
| SDR 4:4:4 | 11.0 / 12.0 ms | 5% | 58% | ~85 W |
| HDR 4:4:4 | 11.1 / 11.3 ms | 6% | 59% | 86 W |

HDR adds nothing measurable. 4:4:4 adds about 3 ms and roughly half again
the encoder load. It fits 4K60, not 4K120. A 3-minute HDR 4:4:4 run held
59.7 fps with no dropped frames. Full numbers:
rust-shine `docs/COLOR_MODES.md`, "Linux NVENC".
