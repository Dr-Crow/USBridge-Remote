# Punktfunk patches

Applied on top of upstream Punktfunk (https://git.unom.io/unom/punktfunk,
made against 6988e5c, 0.42.0) before building the
`punktfunk-host` the agent runs:

    git apply /path/to/agent/patches/punktfunk/*.patch

- `0001-usbridge-usb-broker-bridge.patch` -- hands a USBridge client's raw
  HID device (and, on Windows, its gamepads) to the USBridge USB broker.
  See "USB with Punktfunk as the streamer" in
  `agent/docs/PUNKTFUNK_BACKEND_TODO.md`.
