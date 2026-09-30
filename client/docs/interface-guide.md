# Interface Guide

Once connected — to either a [software Agent](../../agent/docs/README.md) or a [hardware USBridge-KVM](https://github.com/USBridge-Technologies/USBridge-KVM-2.0) — the client provisions four tabs: **Control**, **Devices**, **Snapshots**, **Scripts**. Several capabilities only exist on one of the two target types; each section below says which.

> [!NOTE]
> For the full breakdown of what a software Agent can and can't do versus a hardware KVM, see [Agent vs. Hardware KVM](../../agent/docs/README.md#agent-vs-hardware-kvm) in the Agent docs.

---

## 1. Control Tab (Live View)

The primary interactive workspace — live video from the target, with your local keyboard/mouse routed back to it in real time over the zero-copy [hardware-accelerated video pipeline](./NATIVE_VIDEO_AUDIO.md).

* **Pointer modes:** touchpad (relative) or absolute — see [Mouse & Touchpad Modes](./MOUSE_TOUCHPAD.md) for the exact coordinate math. Multi-display absolute targeting exists in the protocol but is currently disabled in the UI pending calibration work.
* **On hardware KVM only:** BIOS/UEFI-level input works before any OS is loaded — the same USB HID gadget emulation that makes the target see a physical keyboard/mouse at POST. An Agent injects input in software (`SendInput` on Windows, `CGEvent`/Quartz on macOS, direct injection on Linux), so it only works once that machine's own OS is up and its input stack is running.
* **Fullscreen / mobile:** an on-screen [virtual keyboard](./virtual_keyboard.md) is available for touch-only devices.
* **Screenshot tool** (Control footer, next to the clipboard icon): **Screenshot** saves the current frame as a PNG via the OS's own native save panel (not a Fyne dialog); **Copy Text from Screen** (macOS only for now) runs it through the same Vision.framework OCR behind Preview/Photos' Live Text and puts the recognized text on the clipboard. Both grab whatever frame the video pipeline already has decoded — no extra capture, no request to the target.

---

## 2. Devices Tab

What's here depends heavily on what you're connected to:

| Capability | Software Agent | Hardware KVM |
| :--- | :---: | :---: |
| Keyboard / mouse input | ✅ (software injection) | ✅ (USB HID gadget, works pre-OS) |
| Virtual media (`.iso`/`.img` mount) | ❌ | ✅ — [NBD-backed virtual drives](https://github.com/USBridge-Technologies/USBridge-KVM-2.0/blob/main/docs/content/5-remote-disk-image-mounting/mounting-iso-images.md) |
| Power / Reset control | ❌ | ✅ — [Power Management Module](https://github.com/USBridge-Technologies/USBridge-KVM-2.0/blob/main/docs/content/6-hardware-connectivity/power-management-module-control.md), with a 2-second hold-to-confirm before anything fires |
| Internet sharing to the target | ❌ | ✅ — [USB-LAN / RNDIS bridge](https://github.com/USBridge-Technologies/USBridge-KVM-2.0/blob/main/docs/content/2-kvm-vkm/network.md) |

On a hardware KVM, this tab also mounts local `.iso`/`.img` files as virtual USB drives over NBD — the client runs a local NBD server for the file and the appliance connects to it as a client, so nothing has to be uploaded anywhere first.

---

## 3. Snapshots Tab

**Hardware KVM only.** Browses the appliance's own immutable, [Btrfs snapshot-backed storage](https://github.com/USBridge-Technologies/USBridge-KVM-2.0/blob/main/docs/content/4-snapshots-state-management/snapshots-overview.md) — mount the live **Backup Flash** volume (read-write) to add files, or mount any historical snapshot (always read-only) to recover something. A software Agent has no equivalent — it isn't a storage appliance, just the machine being controlled.

---

## 4. Scripts Tab

The Starlark script management section below is **hardware KVM only** — a software Agent has no automation/scripting engine to point at. The MCP Proxy card is not: it's agent-agnostic and works fully with either backend (see [Agent MCP access](../../agent/docs/README.md#mcp--ai-agent-access) for exactly what an Agent connection can and can't do compared to the hardware KVM).

Starlark script management ([full reference](https://github.com/USBridge-Technologies/USBridge-KVM-2.0/blob/main/docs/content/3-bios-in-terminal/scripting-automation.md)):

* **New (SD)** / **New (eMMC)** — create a script on whichever storage you pick.
* Built-in syntax-highlighted editor with **Save** and **Run** right in the toolbar.
* **Delete** — with a confirmation prompt.

**MCP Proxy card** — a **Start**/**Stop** toggle plus a **Copy** button for a local `http://127.0.0.1:8765/api/mcp` endpoint. Starting it runs a small local HTTP server that signs and forwards requests to whichever device is connected — hardware KVM or software Agent — on your behalf, so an AI agent (Claude, or any [MCP](https://modelcontextprotocol.io)-speaking tool) pointed at that local address gets access without implementing the request-signing scheme itself. Against a hardware KVM that's the full tool catalog; against an Agent it's the smaller one (`screen.get_image`/`keyboard.send`/`mouse.action`/`device.info`, plus `ui.parse` and click-at-a-detected-element when the **Local models** toggle below is on — see the Agent doc's MCP section). `ui.parse` defaults to a fast, icon-only pass (sub-second); pass `{"text":true}` for the slower full OCR read. Full reference: [AI Agent Integration (MCP)](https://github.com/USBridge-Technologies/USBridge-KVM-2.0/blob/main/docs/content/3-bios-in-terminal/mcp-ai-agents.md).
