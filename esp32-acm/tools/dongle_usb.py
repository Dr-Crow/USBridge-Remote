#!/usr/bin/env python3
"""Talks to the dongle over its USB control endpoint (Linux, usbfs) -- the way
in when its network link is down and the control port cannot be reached.

  dongle_usb.py status       one-line state
  dongle_usb.py log          firmware log
  dongle_usb.py bootloader   reboot into the ROM download mode (then flash)
  dongle_usb.py reboot

The dongle is found by its serial number string (the chip's MAC), since while
a device is attached it carries that device's VID/PID. Needs write access to
the device node under /dev/bus/usb.
"""
import ctypes
import fcntl
import glob
import os
import sys

MAINT_WINDEX = 0x5542
CHUNK = 1024
USBDEVFS_CONTROL = 0xC0185500


class CtrlTransfer(ctypes.Structure):
    _fields_ = [("bRequestType", ctypes.c_uint8), ("bRequest", ctypes.c_uint8),
                ("wValue", ctypes.c_uint16), ("wIndex", ctypes.c_uint16),
                ("wLength", ctypes.c_uint16), ("timeout", ctypes.c_uint32),
                ("data", ctypes.c_void_p)]


def read(path):
    try:
        with open(path) as f:
            return f.read().strip()
    except OSError:
        return ""


def find():
    """Device node of the dongle: the idle identity, or any device that answers
    the status request."""
    cands = []
    for d in glob.glob("/sys/bus/usb/devices/*/idVendor"):
        d = os.path.dirname(d)
        node = "/dev/bus/usb/%03d/%03d" % (int(read(d + "/busnum")), int(read(d + "/devnum")))
        idle = (read(d + "/idVendor"), read(d + "/idProduct")) == ("303a", "82d7")
        cands.append((not idle, node))
    for _, node in sorted(cands):
        try:
            fd = os.open(node, os.O_RDWR)
        except OSError:
            continue
        try:
            if ctrl(fd, 0xC0, ord("S"), 0, CHUNK).startswith(b"OK usbridge-dongle"):
                return fd
        except OSError:
            pass
        os.close(fd)
    sys.exit("no dongle found (or no access to its device node)")


def ctrl(fd, req_type, request, value, length):
    buf = ctypes.create_string_buffer(max(length, 1))
    t = CtrlTransfer(req_type, request, value, MAINT_WINDEX, length, 1000,
                     ctypes.cast(buf, ctypes.c_void_p))
    n = fcntl.ioctl(fd, USBDEVFS_CONTROL, t)
    return buf.raw[:n]


def main():
    cmd = sys.argv[1] if len(sys.argv) > 1 else "status"
    fd = find()
    if cmd == "status":
        print(ctrl(fd, 0xC0, ord("S"), 0, CHUNK).decode(errors="replace"))
    elif cmd == "log":
        n = 0
        while True:
            part = ctrl(fd, 0xC0, ord("L"), n, CHUNK)
            sys.stdout.write(part.decode(errors="replace"))
            if len(part) < CHUNK:
                break
            n += 1
    elif cmd in ("bootloader", "reboot"):
        ctrl(fd, 0x40, ord("B" if cmd == "bootloader" else "R"), 0, 0)
    else:
        sys.exit(__doc__)


if __name__ == "__main__":
    main()
