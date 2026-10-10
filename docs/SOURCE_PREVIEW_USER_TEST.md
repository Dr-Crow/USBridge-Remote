# Local source preview: experimental Linux user test

Use only an archive whose matching native acceptance receipt says `passed: true`.
A build-only archive or a manager unit test is insufficient. This preview is a
short same-computer Linux/X11 test. It does not provide ordinary Moonlight pairing,
a remote connection, Windows viewing, USB OS attachment, or production parity.

## Prerequisites

- An unprivileged Linux account in a local X11 desktop session. Wayland-only
  desktops and root/service sessions are unsupported by this preview manager.
- The complete extracted preview archive, preserving its executable bits and
  relative layout. Verify its archive SHA-256 against the separately supplied
  trusted checksum before running it.
- FFmpeg installed from the OS/vendor package source, with X11 capture and
  software H264 support. The archive does not bundle FFmpeg.
- The native viewer's shared-library requirements, including X11/OpenGL, FFmpeg,
  Opus, OpenSSL, PulseAudio client and VA/Vulkan libraries used by the inherited
  client build. The preview itself discards decoded silence and opens no audio
  output backend. This is not a self-contained one-click installer.

## Start a short, explicitly approved preview

1. Start `agent/usbridge-agent` normally in the selected local desktop session.
   Finish or stop any existing stock streaming session.
2. Open the settings menu, then **Source preview (experimental)**.
3. Set **Components** to the absolute extracted `components` directory. Copy the
   manifest checksum from its `MANIFEST.sha256`, after verifying the containing
   archive against the trusted published checksum.
4. Set **FFmpeg** to the absolute trusted executable path and **X11 display** to
   the exact local display you intend to capture, such as `:0`. Do not select a
   display you do not have permission to capture.
5. Keep the **128 × 72** profile for the accepted transport test. It captures a
   top-left pixel region rather than scaling the entire desktop. Larger current
   lossless profiles can exceed the bounded frame size on complex content.
6. Review the scope, check the capture approval box, and click **Start approved
   preview**. Each new attempt consumes a new approval and fresh private keys.
7. A separate native viewer opens. Stop with its button, the parent **Stop
   preview** button, or by closing the preview dialog/window. The lease also
   ends automatically in under 30 seconds. Close-to-tray/quit of the parent
   revokes the preview and joins both children.

The `first_frame` protocol event means a decoded image was submitted to the
canvas. The matching CI pixel receipt supplies independent presentation evidence.
If a session fails or a frame is too large, stop and inspect the selected display,
component checksums and supported profile before trying again. Do not disable
manifest verification, TLS/encryption, consent or packet bounds to make it work.

## What acceptance proves

The native fixture runs the actual manager in an agent Go test process, the
verified source binary and the real viewer. It uses two authenticated disposable
Xvfb displays with generated content, inspects actual displayed pixels, and checks
viewer stop, parent-manager stop, expiry, fresh-session restart, canceled startup,
no inherited recording diagnostics, and no configured PulseAudio contact.
It does not yet automate interaction with the shipped agent's parent settings
window. That integration remains a separate acceptance requirement and must not
be reported as complete from the manager fixture alone.
