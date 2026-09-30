<div align="center">

# 💻 USBridge Agent

**Turn any PC or Mac into a remote-controllable USBridge — no hardware required.**

![Windows](https://img.shields.io/badge/Windows-0078D6?logo=windows&logoColor=white)
![macOS](https://img.shields.io/badge/macOS-000000?logo=apple&logoColor=white)
![Linux](https://img.shields.io/badge/Linux-FCC624?logo=linux&logoColor=black)
![License: GPLv3](https://img.shields.io/badge/license-GPLv3-blue)

![USBridge Agent](docs/assets/screenshot_agent.png)

</div>

One lightweight binary for Windows, macOS, and Linux. It shares the exact same codebase and high-performance protocol as the physical [USBridge KVM](https://usbridge.io). Install it on any machine, and the USBridge Client controls it just like it would a real, hardware USBridge device.

---

> 💖 **Patreon Exclusive:** Early access to our custom **Rust-based streaming engine** (Rust-shine). This cutting-edge rewrite includes exclusive features—such as **WebRTC support for the Web Client**, **performance optimizations for macOS**, **working Linux login screens on NVIDIA**, and more—available to our supporters on [Patreon](https://www.patreon.com/USBridge_Technologies). *(Note: The standard upstream C++ Sunshine build does not support WebRTC, so the web client requires this Rust-based version).*

---

## ✨ Why it's Awesome

- **Hardware-Accelerated Sunshine/RustShine Streaming**: Sunshine handles the capture and encoding, seamlessly bundled and auto-launched. The agent leverages Vulkan/Metal for optimal performance. Patreon supporters can unlock our custom **RustShine** engine with WebRTC support.
- **WebRTC Control & Customization**: Enable/disable RustShine's WebRTC signaling endpoint directly from the main window's Permissions column. The setting is persisted in the configuration (`rustshine_webrtc_disabled`), allowing quick activation of the web client link.
- **Streamlined Backend Switching**: An elegant iOS-style toggle switch allows supporters to seamlessly switch between Sunshine and RustShine streaming backends.
- **Built-in Tailscale**: Whether system-wide or userspace, the agent registers itself on your Tailnet on boot. Connect directly on your LAN or securely over Tailscale—nothing else in between.
- **Bank-Grade Security**: API requests are HMAC-SHA256 signed over the master key with a ±60s replay window. The pairing handshake is AES-256-GCM encrypted, using the same robust scheme as the client and the hardware unit.
- **Native Input Injection**: Uses `SendInput` on Windows, `CGEvent`/Quartz on macOS, and direct injection on Linux for 1:1 precise, latency-free mouse and keyboard emulation.
- **Wayland Native**: On Linux, KMS capture (Sunshine or RustShine) needs a single `pkexec` grant. It installs a small root-owned launcher that gives the streamer `CAP_SYS_ADMIN`, and it survives reboots **and streamer updates**, so the portal permission dialog never comes back, even on a remote session. The launcher runs only RustShine builds signed by the release key and only a root-owned copy of Sunshine. See [docs/KMS_CAPTURE_LINUX.md](docs/KMS_CAPTURE_LINUX.md).
- **One-Click Clipboard Setup on Linux**: If neither `xclip` nor `wl-clipboard` is present, the Permissions column offers a one-click, distro-aware install (a "?" button previews the exact `pkexec` command first). On Wayland, both tools get installed and written to together, since desktop compositors don't always mirror the clipboard between native Wayland apps and XWayland ones.
- **Unified Dashboard**: The control window neatly displays your LAN/Tailscale addresses, streaming backend status (including copy-to-clipboard, quick launch, and informational tooltips for WebRTC client links), permission statuses, and Tailscale sign-in all in one place.
- **System Tray**: Closing the window minimizes to a tray icon instead of quitting — the icon itself reflects live status (idle/streaming/needs attention), and its menu gives one-click access to reopening the window, restarting streaming, toggling autostart, and quitting for real. A tray icon stays visible even when the engine is running headless in the background (see [Launch at Login](#-launch-at-login-autostart) below).

## 🚀 Quick Start

1. **Run the agent** on the machine you want to control.
2. It will display a Master QR pairing token alongside its LAN and Tailscale IP addresses.
3. **Open the USBridge Client** anywhere else, scan or enter the token, and connect instantly.

That's the entire setup process.

## 🛠️ Build Instructions

Each script fetches and stages the matching native Sunshine release automatically.

```bash
# macOS
./scripts/build_macos.sh

# Windows — MSYS2 UCRT64
./scripts/build_windows.sh

# Linux
./scripts/build_linux.sh

# or just run it directly (dev mode)
go run ./cmd/usbridge_agent
```

**Environment Variables for Build Customization:**
```bash
USBRIDGE_SKIP_SUNSHINE=1        # Skip bundling Sunshine (useful for fast dev builds)
USBRIDGE_SUNSHINE_FORCE=1       # Force re-download even if already staged
USBRIDGE_SUNSHINE_VERSION=<tag> # Pin to a specific Sunshine release version
```

> **macOS Note:** Grant Screen Recording + Accessibility when prompted, then always launch from the same installed path (`~/Applications/USBridgeAgent.app`). Development builds aren't ad-hoc signed, so re-signing on every rebuild looks like a completely new app to macOS, causing both prompts to return.

## 🗂️ System Tray

Clicking the window's close button minimizes the Agent to a tray icon instead of quitting it — the first time this happens you'll see a one-off "still running in the tray" notification. The icon itself reflects live status (idle / streaming to N client(s) / needs attention), and its menu gives you:

- **Open USBridge Agent** — reopens the window (also triggered by clicking the tray icon itself on most platforms).
- **Restart Streaming**
- **Autostart at Boot** — the same toggle as the main window, mirrored here for convenience.
- **Quit** — the only thing that actually exits the process / stops the engine you're currently looking at.

**Linux (X11 & Wayland):** the tray uses the standard freedesktop StatusNotifierItem protocol, which works out of the box on X11 and on any Wayland compositor that implements it (KDE Plasma, XFCE, Sway with waybar/nwg-panel, etc.). **GNOME ships no tray host at all by default** — install the ["AppIndicator and KStatusNotifierItem Support"](https://extensions.gnome.org/extension/615/appindicator-support/) extension (or newer GNOME's "Tray Icons: Reloaded") to see it. If the Agent detects no tray host is reachable on your session, it automatically falls back to the old behavior (closing the window quits the app) rather than leaving you with an invisible, unquittable process.

**Tray visible even when running headless:** when "Launch at Login" is enabled (see below), a second, lightweight helper also starts at graphical login and attaches to the already-running headless engine — so you always have a visible tray icon and a way to open the window, even though the actual engine (HTTP/Sunshine/Tailscale) is running silently in the background with no window of its own. On Windows this is launched directly by the `USBridgeAgent` service into your active desktop session (no separate autostart entry needed); on Linux/macOS it's a small additional per-user autostart entry (`~/.config/autostart/usbridge-agent-tray.desktop`, or a second LaunchAgent on macOS) installed/removed alongside the main one.

## ⚙️ Launch at Login (Autostart)

The "Launch at Login" checkbox has no stored on/off state of its own. It directly reflects whatever your OS's actual autostart mechanism currently reports (`systemctl is-enabled usbridge-agent.service` on Linux). It starts unchecked on a fresh machine and only becomes enabled when you check it.

Checking it registers **the exact executable you're currently running** as the command to launch at boot:
- **AppImage**: The outer `$APPIMAGE` path (the `.AppImage` file itself, not its ephemeral mount point).
- **Plain binary**: Evaluates via `os.Executable()`.

It always appends the `--headless` flag, ensuring autostart brings up the HTTP, Sunshine, and Tailscale engines silently in the background on every boot. Opening the app normally afterwards simply attaches the GUI to the already-running background instance — or see [System Tray](#-system-tray) above for the automatic, no-click way to get that same GUI/tray back at login.

**Linux specifics:**
On Linux, this installs a **system-wide** (not `--user`) systemd unit at `/etc/systemd/system/usbridge-agent.service` via `pkexec`. This is done deliberately so it can start before any graphical session exists—which is crucial for KMS capture (reading DRM/KMS directly with no compositor or portal required). Re-checking the box from a different path overwrites the unit with the new path; un-checking it removes the unit entirely (along with the tray helper's own autostart entry).

If you move or rename the binary without toggling the checkbox, the installed unit will point at a missing path. Simply toggle it off and on again from the new location, or fix it manually:
```bash
sudo systemctl disable --now usbridge-agent.service
sudo rm -f /etc/systemd/system/usbridge-agent.service
sudo systemctl daemon-reload
rm -f ~/.config/autostart/usbridge-agent-tray.desktop
```

**Windows specifics:**
The `USBridgeAgent` service always runs as `LocalSystem` — deliberately, so it can capture and inject input straight through the lock/sign-in screen, not just an unlocked desktop (see [Platform Notes](docs/README.md#platform-notes-from-the-top-level-readme) for why). A `LocalSystem` service can't show UI in your desktop session on its own, so instead of a separate autostart entry, the service re-homes a small tray-only copy of itself into your active session directly (the same session-broker mechanism the streaming backend already uses to reach your desktop) whenever you log on or the service (re)starts while you're already logged in.

## 🎮 GPU encoder settings (Windows, Linux)

The **Permissions** panel lists the host's GPUs, each with the monitors it drives and a *streaming* mark on the one the running streamer captures, so it's clear which card a setting affects. The NVIDIA settings sit under the NVIDIA card(s) and apply to every NVIDIA GPU; an AMD or Intel card is listed with a note that they don't apply to it (AMF/QSV have their own pipeline). Both settings are written into both streamers' configs; a change restarts the running streamer.

* **NVIDIA power mode** (Windows only) — the NVIDIA Control Panel's power management modes for the streamer's own driver profile: *Max performance* (default, what Sunshine uses), *Consistent performance*, *Adaptive*, *Optimal power*, or *Driver setting* (no override). With max performance the GPU stays at full clocks while streaming; otherwise the driver moves a lightly loaded GPU between low clock levels and the encoder slows down with them (RTX 3090, 2560x1600: host latency drifting between ~4 and ~10 ms instead of a steady ~4.5). No admin rights or UAC prompt. Keys: `nvidia_power_mode` (USBridge Streamer); Sunshine only has `nvenc_latency_over_power`, set to `enabled` for max performance.
* **NVENC two-pass (better picture, more GPU load)** — Windows and Linux — `nvenc_twopass = quarter_res | disabled`, **off by default** (Sunshine and gamestream-server both default it on; this agent opts out by default since the extra GPU load/latency isn't a trade most streaming setups want made for them silently). The quarter-resolution first pass improves quality at low bitrates, at the cost of ~20-30% GPU 3D load and ~0.5 ms of encode time.

This replaces the older **Lock GPU Clocks** checkbox (an elevated NVML clock-lock helper with a UAC prompt on every agent run, Windows only). A lock that was enabled before only still applies while the power mode isn't max performance.

## 📶 Wi-Fi streaming optimization (macOS)

The **Permissions** panel has a **Disable AWDL while streaming** setting: while a Moonlight client is actively connected, the agent brings the `awdl0` interface down and back up once the session ends. AWDL (Apple Wireless Direct Link — the mesh protocol behind AirDrop, Handoff, Sidecar, and Instant Hotspot) shares the same Wi-Fi radio as your normal connection and periodically forces channel-hopping scans even when idle, which measurably hurts a concurrent stream's latency/stability.

Off by default (opt-in). Turning it on installs a narrow `sudoers.d` rule scoped to exactly `ifconfig awdl0 down`/`up` for your user — one admin-password prompt the first time, never again after. The USBridge Client has the same setting (its own footer icon/header prompt), and both apps share the same rule, so granting it once in either app covers the other too.

## 📚 Documentation

**[docs/README.md](docs/README.md)** — full reference, including exactly what the Agent can and can't do compared to the physical [USBridge-KVM 2.0](https://github.com/USBridge-Technologies/USBridge-KVM-2.0) hardware, and a [USB capture/emulation table](docs/README.md#usb-capture--host-side-emulation) covering what's free vs. Pro/Enterprise per device type and OS.

## 📜 License

**GPLv3** — see [`LICENSE`](LICENSE). The agent automatically bundles and interfaces with [Sunshine](https://github.com/LizardByte/Sunshine) (GPLv3).
