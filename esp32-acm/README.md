# esp32-acm

A hardware USB/IP importer: an ESP32 board that does what a VHCI driver
(usbip-win2, Linux `vhci-hcd`) does in software. Plug it into a machine, tell
it which USB/IP exporter to dial, and the exported device shows up on that
machine as a real USB device on a real port, with its own VID/PID and
descriptors, bound by the OS's own driver.

It exists for USBridge agents that have no USB/IP driver to attach to. macOS
is the case that matters: there is no VHCI for it, so a Wacom tablet held by a
remote client cannot be passed through in software at all.

```
USB device ── client (USB/IP exporter) ══ network ══ agent broker ── TCP⟷serial bridge
                                                                       │ framed over the dongle's one CDC-ACM pipe (mux.h)
                                           agent machine's USB port ◄──┘
                                           sees: the cloned device + a CDC-ACM serial port
```

## How it works

The dongle is one composite USB device with two faces:

- **Control pipe** (CDC-ACM, always present, two multiplexed channels — see
  `mux.h`). The agent finds the dongle by its USB serial number (stable
  across attach and detach: `clone_build()` forces the cloned device's
  `iSerialNumber` to point at the dongle's own, not the cloned device's)
  and opens the one serial port TinyUSB enumerates it under. One channel
  carries the control protocol below; the other carries the raw USB/IP
  relay stream — the agent bridges that one to a real TCP connection to the
  exporter, since the dongle itself has no network stack any more (an
  ESP32-S3 generation ago this was CDC-NCM with its own IP address and DHCP
  server; CDC-ACM has no equivalent link-up handshake for the host's driver
  to race against on re-enumeration, which NCM did — see "What works").
- **The cloned device** (only while attached). Its device descriptor,
  configuration, strings, BOS, HID report descriptors and HID feature reports
  are read from the exporter at attach time; every control request and every
  interrupt transfer the host makes is forwarded as a USB/IP `CMD_SUBMIT`
  and answered from the `RET_SUBMIT`. Mass storage (Bulk-Only Transport) is
  followed command by command: the dongle reads the command block, asks the
  exporter for exactly the data phase it announces and streams it to the
  host, then fetches the status.

Attaching or detaching changes the descriptors, so the dongle drops off the
bus and re-enumerates. The control pipe goes down with it and comes back
under the same USB serial number; the agent's bridge to the exporter stays
open the whole time (mirroring a TCP socket riding out a brief link blip,
`mux_relay_read`/`mux_relay_write` just block through the gap — only an
explicit detach or a real failure ends the session), so nothing in flight is
lost.

### The re-enumeration gap

A USB device cannot change what it is without re-enumerating, and the
control pipe is part of that device, so it blinks on every attach and
detach. Measured on Linux, from disconnect to the host having the port back:
about 0.65 s (150 ms off the bus, the hub's connect debounce, reset,
descriptors, the interface coming up). No DHCP/ARP step: CDC-ACM has no
link-up handshake of its own, so the port is usable the moment the host's
driver binds it — this also removes the macOS-specific flakiness CDC-NCM had
here (see "What works"). Only a link that is not a function of the same USB
device (a second USB controller, Wi-Fi) would remove the gap entirely.

The host sets the cloned device up *during* that gap, and some hosts (Linux)
do not finish binding every interface driver instantly. Requests that arrive
while the control pipe is down therefore cannot wait for it:

- HID report descriptors and feature reports are answered from the copies
  read at attach;
- requests without a data stage (`SET_IDLE`, `SET_INTERFACE`, …) and OUT
  requests are accepted and forwarded once the pipe is up;
- any other IN request is stalled immediately instead of hanging until the
  host's timeout.

### What is changed in the clone

The device is reproduced as it is, with these exceptions:

- the CDC-ACM pipe's two interfaces are added (they take the interface
  numbers after the device's own, but come first in the configuration
  descriptor), and the device descriptor's `iSerialNumber` is forced to the
  dongle's own serial-string index instead of whatever the real device's was
  (or none at all) — see "How it works";
- a device-level class (`bDeviceClass` ≠ 0, e.g. the vendor-specific class of
  an Xbox 360 pad) becomes "composite with interface association" — otherwise
  Linux and macOS give the CDC-ACM pipe no driver and the control channel is
  unreachable;
- `bMaxPacketSize0` is 64, endpoints are full speed (64-byte packets,
  intervals in ms), endpoint addresses are remapped where they collide;
- endpoints the hardware cannot back stay in the descriptors but carry no
  data (see Limits). `ATTACH` reports how many: `dead_eps=N`.

While nothing is attached the dongle identifies as `303a:82d7`. While a device
is attached it carries **that device's** VID/PID, so find the dongle by its
address on the link, not by a VID/PID scan.

## Control protocol

Channel 0 of the one CDC-ACM pipe (`mux.h`): one request message per frame,
one reply message, framed as `[1 byte channel][4 bytes length,
big-endian][payload]`.

| Request | Reply |
| --- | --- |
| `HELLO` / `STATUS` | `OK usbridge-dongle proto=2 fw=0.4.0 serial=<mac> state=idle\|attached usb=configured\|unconfigured [busid=… vid=… pid=… urbs=done/submitted] heap=free/minimum up=seconds` |
| `ATTACH <busid>` | `OK vid=…. pid=…. itfs=N eps=N dead_eps=N` or `ERR <reason>` |
| `DETACH` | `OK` |
| `LOG` | one reply frame: the firmware log |
| `BOOTLOADER` | `OK`, then reboots into the ROM download mode for flashing |
| `REBOOT` | `OK`, then reboots |

`ATTACH` expects the agent to have channel 1 (the relay channel) **already**
bridged to a real USB/IP exporter's TCP connection (`OP_REQ_IMPORT`,
protocol 1.1.1) for `busid` — the dongle has no network stack of its own any
more, so reaching the exporter is entirely the agent's job; if it cannot, it
reports the error itself and never sends `ATTACH` at all. Once sent, the
reply comes after the device is imported over that bridge; it appears on the
bus a moment later. Poll `HELLO` until `state=attached usb=configured`.

If the exporter's connection drops, or the host does not reopen the CDC-ACM
port within 10 s of an attach, the dongle unplugs the clone by itself and
returns to `state=idle`.

The USBridge broker drives all of this itself (`usb_passthrough::dongle` in
rust-shine, `--vhci-backend auto|native|dongle`): it finds the port by the
dongle's USB serial number, speaks the framed protocol above, and bridges
channel 1 to the exporter.

## Boards

Each board is its own PlatformIO project in its own folder.

| Folder | Board | Clone limits |
| --- | --- | --- |
| [`esp32-s3-mini`](esp32-s3-mini) | ESP32-S3 mini/SuperMini (ESP32-S3FH4R2, 4 MB flash, one USB-C on the native USB pins) | full speed; 2 IN + 5 OUT endpoints; no isochronous |

What the ESP32-S3's USB core can and cannot do for a cloned device is in
[Limits](#limits).

## Build and flash

```sh
cd esp32-s3-mini
~/.platformio/penv/bin/pio run                      # build

# first time, or a board that does not answer: hold BOOT while plugging in
~/.platformio/penv/bin/pio run -t upload --upload-port /dev/ttyACM0

# afterwards, no button: reboots the running dongle into download mode over
# the control port, then uploads
./flash.sh
```

The board has a single USB port and the firmware uses it as the device port,
so there is no serial console: read the log with `LOG`.

### When the control pipe is down

The dongle also answers a few vendor requests on its control endpoint (to the
device, `wIndex` 0x5542), attached or not, so it can be inspected and
reflashed when the CDC-ACM port does not come up or the host's driver
rejected it:

```sh
tools/dongle_usb.py status       # the STATUS line
tools/dongle_usb.py log
tools/dongle_usb.py bootloader   # then: pio run -t upload
```

`flash.sh` falls back to this by itself. The tool is Linux-only (usbfs) and
needs write access to the device node.

## What works

Checked on a Linux host (kernel 6.17) with the ESP32-S3 mini, firmware 0.3.0,
the devices exported by the USBridge client's own USB/IP exporter and driven
through the dongle's control port.

| Device | State | What was seen |
| --- | --- | --- |
| Wacom Intuos S (CTL-4100, `056a:0374`) | **works** | kernel `wacom` driver binds to the clone (Pen + Pad); position, pressure, distance, touch at 133 reports/s, the tablet's own rate |
| Razer Wolverine V2 (`1532:0a29`, Xbox One GIP), raw | **works** (main interface) | `xpad` binds, GIP handshake over the OUT endpoint; buttons, sticks, triggers, d-pad, about 90 reports/s. Audio interface (isochronous) dead |
| the same pad as the client's Xbox 360 emulation (`045e:028e`) | **works** (main interface) | `xpad` binds; sticks and triggers, about 135 reports/s. Headset interfaces silent |
| synthetic HID gamepad | **works** | `usbhid` binds, about 190 reports/s on a 4 ms endpoint |
| SanDisk flash drive (`0781:55a9`, high speed, Bulk-Only Transport) | **works, slowly** | `usb-storage` binds, capacity and partition table read; 8 MB read back with a matching checksum at 290 kB/s; 512 kB written at 335 kB/s and verified on the stick directly |
| Logitech Bolt receiver (`046d:c548`, 4 HID interfaces) | **partly** | clone is stable, `usbhid` binds all four interfaces, keyboard and mouse interfaces are backed, the two HID++ interfaces are silent. No pointer reports arrived in the test: none reached the exporter on the two backed endpoints either, most likely because this receiver had been switched to Logitech's DJ mode by the Linux driver and reports on its third interface |
| webcam, audio device | **cannot work** | isochronous, and high speed video does not fit a full speed bus. Not attempted |

Also checked: attach and detach through the real USBridge broker
(`--vhci-backend dongle`, synthetic exporter); the exporter disappearing while
attached returns the dongle to idle; reflash over the control port and over
the USB maintenance requests.

Checked on macOS (15, Apple Silicon), firmware 0.4.0, the agent being
`usb-broker --vhci-backend dongle`: synthetic HID gamepad and the real Wacom
Intuos S above (exported by a Linux box's native `usbipd`, relayed by the
agent's TCP↔CDC-ACM bridge) both attach and hold `registered, matched,
active` in `ioreg` indefinitely — the CDC-NCM version of this dongle could
not get past this point reliably on macOS at all (see below). With the
official Wacom driver installed, Wacom Center recognizes the tablet through
the clone and pen position, pressure and the pad buttons all work in a real
app — once bug 3 below (interrupt-IN traffic silently dropped right after
attach) was fixed; before that fix the tablet was visible everywhere but
produced no input at all.

**Why CDC-ACM and not CDC-NCM**: the first version of this dongle (firmware
≤0.3.0) used CDC-NCM for the control/relay link, matching Linux and Windows's
in-box support. On macOS it turned out to be unreliable in two distinct ways
neither visible on Linux:

1. **USB accessory-trust prompt races the 10 s fallback.** Every attach
   gives the clone a different VID/PID than whatever was plugged in before,
   which macOS's `IOAccessoryManager` treats as a brand-new accessory and
   gates behind its USB authorization prompt before handing the
   configuration to any class driver. If that prompt is set to ask and
   nobody answers in time, the dongle's "host never brought the link up"
   fallback fires first and the attach fails, unattended, every time. Fixed
   on the Mac (not in firmware): System Settings → Privacy & Security →
   Allow accessories to connect → Always.
2. **The CDC-NCM link-up notification is flaky on a composite clone.**
   Even past (1), `AppleUSBNCMData::updateLinkStatus: linkStatus 1` — the
   signal the rest of the stack (DHCP, the agent's probe) waited on — simply
   failed to arrive on roughly half of back-to-back attach attempts with an
   otherwise identical composite descriptor; the *idle*, NCM-only descriptor
   never showed this. Looked like a timing/race condition inside Apple's own
   `com.apple.driver.usb.cdc.ncm`, not something fixable from the dongle's
   side. `usb-broker`'s retry-the-whole-attach mitigation
   (`DONGLE_ATTACH_RETRIES` in `dongle_attach`) papered over it well enough
   to be usable, but CDC-ACM removes the failure mode structurally: a
   CDC-ACM port has no analogous link-up handshake to race, so there is
   nothing here for a flaky driver internal to desync.
3. **A write right after reconnect can be dropped by the host before
   anything has reopened the new device node.** `tud_cdc_n_connected()`
   flips true the instant the USB bus reconfigures after the one-time
   re-enumeration attach causes, but on macOS the device node's name
   changes on every re-enumeration (`cu.usbmodem<serial>1` ↔
   `...2`), and the host process needs a moment to notice the old node is
   gone and open the new one. `tx_task` (`usbip.c`) used to resume writing
   queued CMD_SUBMITs the instant `mux_connected()` went true, which on
   macOS landed some of the very first writes — including the tablet's own
   interrupt-IN endpoint resubmission — in that reopen gap, where nothing
   had the port open yet to buffer them into: they were just gone, no error
   on either side. Control-channel traffic (`HELLO`/`STATUS`) was unaffected
   because `ctrl.c` only ever replies to a request the host just sent, which
   can't happen before the host has reopened the port. Symptom: the clone
   enumerates and the exporter responds to every descriptor-caching request
   fine, but `urbs=0/N` in `STATUS` never advances no matter how long the
   real device is used — the one and only interrupt-IN submission sent
   right after attach silently vanished, and macOS's single-outstanding-URB
   model means nothing else gets submitted after it until it completes.
   Fixed by having `tx_task` wait an extra ~0.7 s (the same settle time "The
   re-enumeration gap" documents) after `mux_connected()` goes true before
   trusting a write will actually land.

Not checked yet:

- Windows as the host (a device with a vendor driver that owns the whole
  device, like `xusb22` for an Xbox 360 pad, may leave the CDC-ACM pipe
  without a driver; COM port stability across re-enumeration assumed from
  Windows normally keying it to the device's USB serial number, not
  confirmed on real hardware);
- mass storage with a mounted filesystem and real file copies; a data-out
  phase above 128 kB (forwarded in pieces instead of as one URB);
- the "host never brought the control pipe up" watchdog firing on its own
  with the new transport.

## Limits

| Limit | Why | Effect |
| --- | --- | --- |
| 2 IN endpoints carry data | the ESP32-S3 has four TX FIFOs besides EP0's and the network link uses two | a device's third and later IN endpoints are *silent*: they answer polls with NAK, so the host sees an idle endpoint, but nothing comes out of them. Two such endpoints at most; beyond that they are dead (no answer) |
| no isochronous | not implemented, and a full speed bus has no room for it next to the link | audio and video interfaces are dead; selecting their alternate setting is refused |
| full speed (12 Mbit/s) | the ESP32-S3's USB core | high speed devices are cloned as full speed ones |
| mass storage about 0.3 MB/s | every byte crosses the same full speed bus twice, in over the link and out to the host | fine for small files, not for copying gigabytes |
| the control pipe blinks on attach and detach, about 0.65 s on Linux | the cloned device and the control pipe are one USB device | see "The re-enumeration gap" |
| bulk IN outside mass storage | a device cannot tell how much the host asked for on a bulk IN endpoint; for Bulk-Only Transport the command block says so, for anything else there is only a best-effort stream | printers, serial adapters, vendor bulk protocols are untested and may not work |
| report rate bounded by the round trip | one interrupt-IN URB is outstanding per endpoint, as with a software VHCI | over a slow network the rate drops to one report per round trip |
| control OUT with data is acknowledged early | the status stage cannot be held back in this USB stack | a stall from the device on e.g. `SET_REPORT` is not reported to the host |
| feature reports during the gap are the attach-time copies | the device cannot be asked while the control pipe is down | |
| one device at a time | one USB device port | |
| `303a:82d7` | placeholder PID under Espressif's VID | |
