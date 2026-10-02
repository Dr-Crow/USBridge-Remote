#pragma once

#define DONGLE_FW_VERSION "0.4.0"
// Bumped whenever the control protocol (ctrl.c) changes incompatibly.
#define DONGLE_PROTO 2

// Identity while no device is attached. 0x303A is Espressif's VID; the PID
// is a placeholder until one is allocated for this product.
#define DONGLE_IDLE_VID 0x303A
#define DONGLE_IDLE_PID 0x82D7

// The one CDC-ACM pipe (mux.h): the agent finds the dongle by its USB serial
// number (the same one reported in STATUS) and opens the port TinyUSB
// enumerates it under -- no network addressing involved any more.

// ESP32-S2/S3's USB core has endpoint numbers 1..6, but only four TX FIFOs
// besides EP0's, and an IN endpoint uses the FIFO of its own number -- so IN
// endpoints are limited to numbers 1..4. The CDC-ACM pipe takes IN 3 (data)
// and 4 (notif) and OUT 6; the cloned device gets IN 1..2 and OUT 1..5. (The
// same split CDC-NCM used -- a CDC-ACM function costs the same two IN FIFOs
// as CDC-NCM did, which is why there is only one of it: two would leave none
// for the cloned device.)
#define CDC_EP_NOTIF 0x84
#define CDC_EP_IN 0x83
#define CDC_EP_OUT 0x06
#define CLONE_IN_NUM_MAX 2
#define CLONE_OUT_NUM_MAX 5
// IN endpoint numbers 5 and 6 exist but have no TX FIFO: they can be switched
// on and will answer every poll with NAK, like an endpoint with nothing to
// say. An IN endpoint of the cloned device that cannot be backed goes there
// if it can, so that the host driver polling it sees an idle endpoint rather
// than a broken device.
#define CLONE_SILENT_IN_FIRST 5
#define CLONE_SILENT_IN_LAST 6

// String indices of our own strings, kept clear of the low indices a cloned
// device uses for its own.
#define STRIDX_CDC_ITF 0xF1
// The device descriptor's iSerialNumber is forced to this index in both idle
// and clone mode (clone_build overwrites whatever the real device's own
// serial-string index was): the agent finds the dongle's CDC-ACM port by
// this serial number, and it has to stay the dongle's own, stable one no
// matter what gets cloned -- a cloned device with no serial string (or a
// different one) would otherwise change the port's identity out from under
// the host mid-attach.
#define STRIDX_DONGLE_SERIAL 0xF2

#define CTL_BUF_SZ 4096

// Maintenance requests on the control endpoint, for when the link is down
// and the control port cannot be reached: vendor requests to the device with
// this wIndex are answered by the dongle itself, attached or not, and never
// forwarded to the cloned device.
//   IN  'S'            status line (text)
//   IN  'L', wValue=n  firmware log, n-th MAINT_CHUNK bytes (n=0 takes a new snapshot)
//   OUT 'B'            reboot into the ROM download mode
//   OUT 'R'            reboot
#define MAINT_WINDEX 0x5542
#define MAINT_CHUNK 1024
