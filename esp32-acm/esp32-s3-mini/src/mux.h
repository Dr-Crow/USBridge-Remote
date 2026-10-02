#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

// One CDC-ACM pipe (the ESP32-S3's USB core has only four IN FIFOs besides
// EP0's; replacing CDC-NCM with two independent CDC-ACM functions would have
// used all four and left none for the cloned device) carrying two logical
// channels, framed as:
//
//   [1 byte channel][4 bytes length, big-endian][length bytes payload]
//
// Channel 0 is the control protocol (ctrl.c): short, discrete request/reply
// messages, the ACM counterpart of the old TCP control port. Channel 1 is
// the USB/IP relay (usbip.c): an arbitrary-length byte stream, the ACM
// counterpart of the old TCP socket to the exporter. The host multiplexes
// the same way (see dongle.rs).
void mux_init(void);

// True once the host has the serial port open (DTR asserted). Nothing is
// sent on either channel before this.
bool mux_connected(void);

// ---- control channel --------------------------------------------------
// Blocks up to timeout_ms for one complete message; returns its length, 0 on
// timeout, -1 if the port dropped. `max` must be large enough for any ctrl.c
// request/reply (a few hundred bytes).
int mux_ctrl_recv(uint8_t *buf, size_t max, uint32_t timeout_ms);
bool mux_ctrl_send(const uint8_t *buf, size_t len);

// ---- relay channel ------------------------------------------------------
// Byte-stream semantics, like the old read_full/write_full on a TCP socket.
// Returns false on a dropped port (the caller treats that like a closed
// socket, same as before).
bool mux_relay_read(void *buf, size_t n);
bool mux_relay_write(const void *buf, size_t n);
// Drops anything buffered from a previous session (called before a fresh
// attach starts reading/writing the relay channel).
void mux_relay_reset(void);
// Ends the logical relay session without touching the physical ACM
// connection (the control channel must keep working): wakes a blocked
// mux_relay_read with a `false` return, the same way the old code's
// shutdown() on the TCP socket woke a blocked recv().
void mux_relay_abort(void);
