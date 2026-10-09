# Client TODO

See [docs/GAMEPADS.md](docs/GAMEPADS.md) for what is supported today.

## Streamer benchmark

See [docs/STREAMER_BENCHMARK.md](docs/STREAMER_BENCHMARK.md) for what it does today.

- **Browser/WebRTC benchmark path.** `service.BenchSupported()` is hard-wired
  `false` on the web (wasm) build, so the gear menu's *Run benchmark* item
  doesn't exist there today. The whole per-frame recorder
  (`internal/service/bench_recorder.go`, `bench_frames.c`) is built on
  `dr_submit`'s `DECODE_UNIT` callback from the native moonlight decode
  path, which the web client doesn't have (it decodes in the browser over
  WebRTC, see `webrtcweb/client_wasm.go`) -- so this needs a second
  `BenchRecorder`-shaped implementation sourced from `getStats()` polling
  (bytesReceived, framesDecoded, packetsLost, jitter -- see
  `net_graph_wasm.go`'s existing `netGraphWasmNetworkStats` for the same
  translation already done for the live HUD) rather than a small tweak to
  the existing one.
  - Once that recorder exists, the benchmark setup dialog
    (`showBenchmarkSetup` in `internal/gui/benchmark_dialog.go`) should
    offer a third, browser-only backend choice -- "USBridge (WebRTC)" --
    and on a wasm build show *only* that one, not the desktop pair
    (Sunshine / USBridge Streamer classic transport), since a browser can
    only ever reach the host over WebRTC in the first place.
  - Whether "USBridge (WebRTC)" is really a distinct agent-side backend
    kind or just the existing `"rustshine"` backend measured through a
    different client transport needs deciding before touching
    `BenchStatus`/`BenchSetBackend` -- leaning toward the latter (no new
    agent API surface, `showBenchmarkSetup` just changes which client is
    doing the measuring).

## Gamepads

- **Verify on hardware what is only unit-tested:** two pads at once through the real
  client (controller numbers, the departure event, rumble routed by controller
  number), the PS button as Guide on the Raiju TE (button 13 by the DS4 descriptor,
  not pressed yet), XInput capture input on a Razer Wolverine V2.
- **Touchpad-as-mouse on Linux and macOS.** Windows reads the DS4 touchpad from the HID
  input report. On Linux the touchpad is a separate evdev device
  (`Wireless Controller Touchpad`, `ABS_MT_*`), on macOS an IOKit element; the mouse
  gain (about 0.6 counts per unit) and a tap-to-click option are not configurable.
- **Rumble for more pads.** Only the Raiju TE and Sony DualShock 4 ids have an HID
  output-report protocol (`hidRumbleProtocols`). Other Raiju models
  (`1532:1000/1004/1009/100A`) map like the TE but are not enabled for rumble until
  someone has felt them vibrate. DualSense (`054C:0CE6`) uses a different report and
  Switch Pro another; neither is written.
- **Hot-plug.** The list of pads is read at start and on *Refresh*; watch for
  device arrival (Windows `RegisterDeviceNotification` / evdev inotify).
- **Send the upper 16 button bits.** The cgo senders take `unsigned short` buttons,
  so paddles and Misc cannot go over the wire (the touchpad is sent as a mouse
  instead).
- **libwdi WinUSB leftovers.** The WinUSB package that USB passthrough installs
  (`razer_raiju_..._(interface_3)`, `oem*.inf`) hides the pad from HID/WinMM/XInput
  and is not rolled back when the device is released. Passthrough should restore the
  original binding itself.

## Keyboard input modes

The Control footer has a keyboard input mode menu: **Keys** (raw keys by physical
position, the host layout decides) and **Characters** (the client layout decides,
the character is typed on the host). Done and tested for a Windows host; the rest:

- **Characters mode for Linux and macOS hosts through the clipboard.** Today ASCII
  goes as VK+Shift (right only while the host layout is Latin) and anything else
  through Sunshine `unicode()` (on Linux the IBus Ctrl+Shift+U trick, which most
  non-GTK apps don't understand). Type through the host clipboard instead: send
  the text over the clipboard channel, press Ctrl+V (Cmd+V on macOS), put the
  previous host clipboard back. Batch fast typing into one paste.
- **Check on a real macOS client:** the double-typed `0`/`8`/`A`/`S`/`E`/`R` fix
  (`input.NormalizeScanCode`, kVK table) and Keys mode. Only unit-tested.
- **Check on a real Linux client:** Keys mode and the xkb navigation-cluster table.
- **Web (wasm) client and mobile hardware keyboards** have no Keys mode yet.
- **Clipboard menu on mobile.** Send/Get are in the desktop footer only; mobile
  keeps just the auto-sync toggle in the mouse menu.

## Camera uplink (client camera -> remote PC)

The camera row of the HID / Audio card (`disk_widget_uplink.go`,
`platform/uplink_camera_linux.c`) sends H.264 over ENet
(`LiSendCameraFrame`); the agent's streamer shows it as a USB webcam.

- **Software encode only, for now.** `open_encoder` tries `libx264`
  (ultrafast, zerolatency, constrained baseline, ~2.5 Mbit/s) *first*; the
  hardware encoders in its list (`h264_nvenc`, `h264_qsv`, `h264_amf`) are
  only fallbacks, and there's no VAAPI path. At 720p30 that is roughly
  5-10% of one core. Put the hardware encoders first (keeping `libx264` as
  the fallback), add `h264_vaapi` on Linux (needs frames uploaded to a
  VAAPI surface) and `h264_mf` on Windows, and check that each still emits
  SPS/PPS with every keyframe and honours a forced keyframe.
- **The host side decodes in software too**: rust-shine's camera sink uses
  openh264 (CPU), then scales and converts to YUY2 on the CPU. Fine for one
  720p30 camera; a hardware decoder (D3D11VA/MF, VAAPI, VideoToolbox) would
  still copy each frame to system memory for the USB camera.
- **Linux only.** Windows (Media Foundation capture) and macOS
  (AVFoundation + VideoToolbox) have no capture yet -- `CameraSupported`
  is a stub there, same as the microphone and MIDI. See rust-shine's
  WINDOWS_TODO.md / MACOS_TODO.md for the host side.
