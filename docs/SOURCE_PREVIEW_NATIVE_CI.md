# Source preview native CI findings

The preview is experimental. A compiled viewer or decoded-frame callback is not
proof that video was presented. The native gate retains independent X11 pixel
inspection, viewer-close/parent-stop/expiry cleanup, fresh-session restart,
consent, private stdin keys, and disabled recording diagnostics.

## Inherited FFmpeg SDK compatibility failure

The first preview run at `e3827e22a8eb68c66d23742028ebcd300e983d3a`
reached the native client build and failed in the inherited
`client/internal/platform/uplink_camera_linux.c`: Ubuntu 22.04's FFmpeg 4.x SDK
uses `FF_PROFILE_H264_CONSTRAINED_BASELINE`, while that file referenced the
newer `AV_PROFILE_H264_CONSTRAINED_BASELINE` name. The original file was verified
byte-for-byte against the pre-preview `fd5788ac809b89fbf84318a9b888d6b728e42bf8`
GitHub content (blob `2315b7d94bb45e5c0b467beeca753eb1cc3bce88`). This is inherited
upstream/client SDK compatibility, not a new preview runtime regression.

The isolated fix aliases the newer name to the older macro only when absent.
Both definitions have value `66 | (1 << 9)`; no encoder setting changes.
Primary references: [FFmpeg 4.4 avcodec.h](https://ffmpeg.org/doxygen/4.4/avcodec_8h_source.html)
and [current FFmpeg defs.h](https://ffmpeg.org/doxygen/trunk/defs_8h_source.html).
Legacy and modern macro branches pass separate C11 `-Wall -Werror` syntax checks.
Actual full native build and renderer acceptance remain CI gates.

## Acceptance fixture lifetime

The two disposable Xvfb servers use `-noreset` so their explicit generated
background and display state survive between short setup clients and the
actual source/viewer connection. They remain authenticated, local-socket-only,
and owned/joined by the job. This fixture correction does not weaken pixel
matching or allow user desktop capture.
