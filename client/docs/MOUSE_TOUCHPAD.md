# Pointer control: touchpad / touchscreen / absolute

## Description

The client can control the pointer on the remote machine over the video using three main modes:

- **`mouse` (touchpad / relative)** — movement is sent as `dx/dy` (HID mouse).
- **`touchscreen` (touchscreen / absolute + touch)** — movement and clicks are sent as `x/y` + `tip` (HID touchscreen).
- **`absolute` (absolute / absolute without touch)** — position is sent as `x/y` without touch; clicks are sent as separate mouse clicks.

## Available modes

Currently available:
1. **Touchpad** — standard relative-movement mode.
2. **Absolute** — absolute positioning mode (Single Display).

> **Note:** **Abs L/2** and **Abs R/2** modes (for multi-monitor systems) are temporarily disabled pending further work on the coordinate calibration algorithm.

## API and security

All mouse control commands (`POST /api/mouse`) now require:
1. A valid HMAC-SHA256 signature in the headers.
2. An active sync session (Master QR Sync).

### Request format
```json
{
  "action": "move|click|scroll|touch|touch_position",
  "dx": 0,
  "dy": 0,
  "x": 0,
  "y": 0,
  "button": 1,
  "tip": false
}
```
Coordinate range for absolute modes: **0..4095**.

## Keyboard: text mode vs keys mode

The client sends physical keystrokes in one of two ways.

* **Keys mode** sends the physical key position (scan code → VK). What the
  host types depends on the host's own layout, the same as a local
  keyboard plugged into it.
* **Text mode** sends the character the client's own layout produced.
  * ASCII goes as a VK + Shift press.
  * A non-ASCII character goes as Moonlight UTF-8 text. That works on
    RustShine (it switches the KDE layout itself), and on Windows and
    macOS hosts (native Unicode injection).
  * Sunshine on a **Linux** host is the exception. Its Unicode injection
    is an IBus Ctrl+Shift+U hex sequence, so KDE and most non-GTK apps
    print the hex digits (typing "и" produced "438"). For Sunshine on Linux
    the client instead asks the agent to switch the host layout
    (`POST /api/keyboard/layout`, KDE Wayland) to Russian, then presses the
    key that character sits on in ЙЦУКЕН. It switches back to English
    before Latin text.
  * Where the host can't switch layouts (not KDE), Cyrillic falls back to
    UTF-8 text.
