#pragma once

#include <stdbool.h>
#include <stddef.h>
#include <stdint.h>

#include "config.h"
#include "mux.h"
#include "tusb.h"

// ---- logbuf.c -------------------------------------------------------------
// The board has one USB port and it is the gadget, so there is no serial
// console once the firmware runs. Logs are kept in RAM and read through the
// control port (LOG).
void logbuf_init(void);
size_t logbuf_copy(char *dst, size_t max);

// ---- usb_core.c -----------------------------------------------------------
void usb_core_start(void);
// Runs fn(arg) inside the TinyUSB task and waits for it. Everything that
// touches TinyUSB or the clone's endpoint state goes through here or
// usbd_defer_func -- the stack is not thread-safe.
bool usb_call_sync(void (*fn)(void *), void *arg);
// Drops off the bus, runs `swap` (may be NULL) in the TinyUSB task while
// detached, and comes back with whatever descriptors are current by then.
void usb_core_reenumerate(void (*swap)(void *), void *arg);
const uint8_t *usb_core_base_mac(void);

// ---- clone.c --------------------------------------------------------------
#define CLONE_EP_MAX 12
#define CLONE_FEATURE_MAX 48
#define CLONE_STR_MAX 12
#define CLONE_CFG_MAX 1024
#define CLONE_ITF_MAX 16
#define CLONE_HID_MAX 4

typedef struct {
    uint8_t gaddr;      // endpoint address on our bus
    uint8_t raddr;      // endpoint address on the exported device
    uint8_t type;       // TUSB_XFER_*
    uint8_t interval;
    uint8_t itf;        // interface it belongs to
    uint8_t alt_mask;   // bit n: part of alternate setting n (settings above 7 share bit 7)
    uint16_t mps;       // wMaxPacketSize on our (full speed) bus
    uint16_t urb_len;   // IN: size of each CMD_SUBMIT; OUT: receive size
    uint8_t *buf;       // endpoint transfer buffer, urb_len bytes
    uint8_t *hold;      // IN: data of the last RET_SUBMIT, urb_len bytes
    uint16_t hold_len;
    int32_t hold_status;
    // A reply larger than hold is handed over piece by piece: the reader
    // fills hold, waits for hold_full to drop, fills it again.
    volatile bool hold_full;
    bool hold_last;     // this piece ends the reply
    // Listed in the descriptors but not backed by a hardware endpoint
    // (isochronous, or no endpoint left). Only allowed in alternate settings
    // other than 0; selecting such a setting is refused.
    bool phantom;
    bool silent;        // phantom, but answers polls with NAK
    bool bot;           // bulk endpoint of a Bulk-Only Transport interface
    bool zlp;           // IN: a zero-length packet follows the running transfer
    int32_t out_status; // OUT: status of the last RET_SUBMIT
    volatile bool open;       // endpoint is open on the bus
    volatile bool busy;       // IN: transfer to the host running; OUT: armed
    volatile bool ready;      // IN: hold has data waiting for the endpoint
    volatile bool in_flight;  // IN: a CMD_SUBMIT is outstanding
    volatile bool halted;
    uint32_t retry_at;        // IN: earliest tick for the next CMD_SUBMIT
} clone_ep_t;

typedef struct {
    volatile bool active;     // descriptors below are what the bus shows
    char busid[32];
    uint16_t vid, pid;
    uint8_t speed;            // USB/IP speed code of the exported device
    uint8_t num_itf;          // interfaces of the cloned device
    uint8_t cfg_value;
    uint8_t dev_desc[18];
    uint8_t cfg[CLONE_CFG_MAX];
    uint16_t cfg_len;         // header + NCM + clone
    uint16_t clone_off;       // where the clone's own interfaces start in cfg
    uint8_t *bos;
    uint16_t bos_len;
    uint16_t langid_desc[8];
    struct {
        uint8_t idx;
        uint16_t *desc;
    } str[CLONE_STR_MAX];
    uint8_t n_str;
    // HID report descriptors by interface number, read at attach.
    struct {
        uint8_t itf;
        uint16_t len;
        uint8_t *desc;
    } hid[CLONE_HID_MAX];
    uint8_t n_hid;
    // HID feature reports read at attach, to answer GET_REPORT while the
    // link is down.
    struct {
        uint8_t itf;
        uint8_t id;
        uint16_t len;
        uint8_t *data;
    } feature[CLONE_FEATURE_MAX];
    uint8_t n_feature;
    clone_ep_t ep[CLONE_EP_MAX];
    uint8_t n_ep;
    uint8_t n_phantom;        // endpoints listed but not backed
} clone_t;

extern clone_t g_clone;

// Fills g_clone from the exported device's descriptors (config descriptor is
// patched for our bus and gets the NCM function appended). Not active yet.
bool clone_build(const uint8_t *dev, const uint8_t *cfg, uint16_t cfg_len,
                 uint8_t speed, char *err, size_t errlen);
bool clone_add_string(uint8_t idx, const uint8_t *desc, uint16_t len);
void clone_set_langids(const uint8_t *desc, uint16_t len);
void clone_set_bos(const uint8_t *desc, uint16_t len);
bool clone_add_hid_report(uint8_t itf, const uint8_t *desc, uint16_t len);
bool clone_add_feature(uint8_t itf, uint8_t id, const uint8_t *data, uint16_t len);
// Lists the feature reports a HID report descriptor declares: report id (0
// if the device does not use ids) and GET_REPORT length. Returns how many.
int hid_feature_reports(const uint8_t *desc, uint16_t len, uint8_t *id, uint16_t *size, int max);
// Lists the HID interfaces and their report descriptor lengths; returns how many.
int clone_hid_interfaces(uint8_t *itf, uint16_t *len, int max);
// Collects the string indices the descriptors reference (0-terminated).
void clone_string_indices(uint8_t *out, size_t max);
void clone_clear(void);
const uint16_t *clone_string(uint8_t idx);
// TinyUSB task.
void clone_tick(void);
void clone_in_done(void *ep_index);      // deferred from the USB/IP reader
void clone_out_sent(void *ep_index);     // deferred from the USB/IP writer
void clone_out_done(void *ep_index);     // deferred from the USB/IP reader
void clone_ctrl_done(void *reply);       // deferred, takes a ctrl_reply_t*
uint8_t *clone_ctl_buf(void);
uint32_t clone_ctl_gen(void);

typedef struct {
    uint32_t gen;
    int32_t status;
    uint16_t len;
} ctrl_reply_t;

// ---- usbip.c --------------------------------------------------------------
// Imports busid over the relay channel (mux.h) -- the host is expected to
// already have it bridged to the real exporter by the time this is called --
// reads the descriptors and builds g_clone. On success the URB session is
// running and the caller swaps the clone onto the bus.
bool usbip_attach(const char *busid, char *err, size_t errlen);
void usbip_stop(void);
bool usbip_running(void);
// TinyUSB task. `gen` identifies the control request waiting for the reply;
// want_reply=false is fire-and-forget.
bool usbip_submit_ctrl(const tusb_control_request_t *req, const uint8_t *out,
                       uint16_t out_len, uint32_t gen, bool want_reply);
// `len` is how much to ask the device for.
bool usbip_submit_in(uint8_t ep_index, uint32_t len);
bool usbip_submit_out(uint8_t ep_index, const uint8_t *data, uint16_t len);
// Same, for a malloc'd buffer of any size that is handed over (freed once
// written, or at once on failure).
bool usbip_submit_out_owned(uint8_t ep_index, uint8_t *data, uint32_t len);
// Unlinks the control URB still waiting for a reply (the host gave up on it).
void usbip_cancel_ctrl(void);
void usbip_stats(uint32_t *submitted, uint32_t *completed);

// ---- ctrl.c ---------------------------------------------------------------
void ctrl_start(void);
// The USB/IP session ended on its own (exporter closed, link lost).
void ctrl_session_lost(void);
// Reboots shortly, into the ROM download mode if asked. Callable from any task.
void ctrl_request_reboot(bool bootloader);
// One-line state for STATUS and the maintenance request.
void ctrl_status_line(char *out, size_t n);
