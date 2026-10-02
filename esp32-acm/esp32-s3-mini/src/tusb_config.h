#pragma once

#define CFG_TUSB_RHPORT0_MODE (OPT_MODE_DEVICE | OPT_MODE_FULL_SPEED)
#define CFG_TUSB_OS OPT_OS_FREERTOS
// ESP-IDF keeps the FreeRTOS headers under freertos/.
#define CFG_TUSB_OS_INC_PATH freertos/

#ifndef CFG_TUSB_DEBUG
#define CFG_TUSB_DEBUG 0
#endif

#define CFG_TUSB_MEM_SECTION
#define CFG_TUSB_MEM_ALIGN __attribute__((aligned(4)))

#define CFG_TUD_ENABLED 1
#define CFG_TUD_ENDPOINT0_SIZE 64
#define CFG_TUD_INTERFACE_MAX 16

// One CDC-ACM pipe instead of CDC-NCM: in-box everywhere NCM was (macOS,
// Linux, Windows) but with no link-up handshake for the host's driver to
// race against on re-enumeration (see mux.h). Control and USB/IP relay
// traffic are both framed over this one pipe -- the chip's USB core only
// has four IN FIFOs besides EP0's, and two independent CDC-ACM functions
// would use all four, leaving none for the cloned device.
#define CFG_TUD_NCM 0
#define CFG_TUD_ECM_RNDIS 0

#define CFG_TUD_CDC 1
// Generous buffers so a whole mux frame (e.g. a cached config descriptor or
// a mass-storage data phase chunk) rarely needs more than one TinyUSB-level
// write/read to move.
#define CFG_TUD_CDC_TX_BUFSIZE 4096
#define CFG_TUD_CDC_RX_BUFSIZE 4096
#define CFG_TUD_MSC 0
#define CFG_TUD_HID 0
#define CFG_TUD_MIDI 0
#define CFG_TUD_VENDOR 0
