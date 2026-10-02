// The cloned device: its descriptors as shown on our bus, and the TinyUSB
// class driver that turns what the host does to it into USB/IP URBs.
//
// Everything below except clone_build/clone_add_string/clone_set_* (which
// run before the clone is put on the bus) runs in the TinyUSB task.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/task.h"

#include "device/usbd_pvt.h"
#include "dongle.h"

static const char *TAG = "clone";

#define USBIP_SPEED_HIGH 3
#define EPIPE_STATUS (-32)
// Pause before asking again after an IN URB failed for a reason other than
// a stall, so a dead exporter is not hammered.
#define IN_RETRY_MS 100
#define BULK_URB_LEN 2048
// Size of the pieces a mass-storage data phase moves in; a multiple of every
// bulk packet size.
#define BOT_CHUNK 4096
// A data-out phase up to this size is collected and forwarded as one URB (the
// way a host controller driver would send it); a larger one goes in pieces.
#define BOT_OUT_WHOLE_MAX (128 * 1024)

// Device IN endpoint control registers of the USB core (DWC2).
#define DIEPCTL(n) (*(volatile uint32_t *)(0x60080900UL + (n) * 0x20))
#define DIEPCTL_USBAEP (1u << 15)
#define DIEPCTL_SNAK (1u << 27)
#define DIEPCTL_SD0PID (1u << 28)

clone_t g_clone;

// The control request waiting for the exporter's answer. s_gen changes with
// every SETUP, so an answer that arrives after the host moved on is dropped.
static tusb_control_request_t s_req;
static volatile uint32_t s_gen;
static bool s_ctl_wait;
static uint8_t s_ctl_buf[CTL_BUF_SZ];
static bool s_cfg_sent;
// Alternate setting the host selected, per interface of the clone.
static uint8_t s_cur_alt[CLONE_ITF_MAX];

// Whether the endpoint belongs to the setting its interface is in now.
static bool ep_selected(const clone_ep_t *e) {
    uint8_t alt = e->itf < CLONE_ITF_MAX ? s_cur_alt[e->itf] : 0;
    return e->alt_mask & (1u << (alt > 7 ? 7 : alt));
}

uint8_t *clone_ctl_buf(void) { return s_ctl_buf; }
uint32_t clone_ctl_gen(void) { return s_gen; }

// ---- descriptors ----------------------------------------------------------

static void seterr(char *err, size_t n, const char *msg) {
    if (err && n) {
        snprintf(err, n, "%s", msg);
    }
}

static clone_ep_t *ep_by_raddr(uint8_t raddr) {
    for (int i = 0; i < g_clone.n_ep; i++) {
        if (g_clone.ep[i].raddr == raddr) {
            return &g_clone.ep[i];
        }
    }
    return NULL;
}

static clone_ep_t *ep_by_gaddr(uint8_t gaddr) {
    for (int i = 0; i < g_clone.n_ep; i++) {
        if (g_clone.ep[i].gaddr == gaddr) {
            return &g_clone.ep[i];
        }
    }
    return NULL;
}

static bool addr_taken(uint8_t addr) {
    return addr == CDC_EP_NOTIF || addr == CDC_EP_IN || addr == CDC_EP_OUT ||
           ep_by_gaddr(addr) != NULL;
}

// Picks the address an endpoint gets on our bus: its own when that fits
// (within what config.h leaves that direction, and still free), otherwise
// the lowest free one. 0 when the direction is full.
static uint8_t alloc_gaddr(uint8_t raddr) {
    uint8_t dir = raddr & 0x80;
    uint8_t max = dir ? CLONE_IN_NUM_MAX : CLONE_OUT_NUM_MAX;
    uint8_t num = raddr & 0x0F;
    if (num >= 1 && num <= max && !addr_taken(raddr)) {
        return raddr;
    }
    for (num = 1; num <= max; num++) {
        if (!addr_taken(dir | num)) {
            return dir | num;
        }
    }
    return 0;
}

// An address for an endpoint that cannot carry data. An interrupt or bulk IN
// endpoint gets one of the FIFO-less endpoint numbers if one is left
// (*silent): it will NAK. Anything else only exists in the descriptors: its
// own address if nothing else has it, otherwise one the hardware does not
// have at all.
static uint8_t alloc_phantom_addr(uint8_t raddr, bool can_nak, bool *silent) {
    uint8_t dir = raddr & 0x80;
    *silent = false;
    if (dir && can_nak) {
        for (uint8_t num = CLONE_SILENT_IN_FIRST; num <= CLONE_SILENT_IN_LAST; num++) {
            if (!addr_taken(dir | num)) {
                *silent = true;
                return dir | num;
            }
        }
    }
    if (!addr_taken(raddr) && (raddr & 0x0F) > CLONE_SILENT_IN_LAST) {
        return raddr;
    }
    if (!dir && !addr_taken(raddr)) {
        return raddr;
    }
    for (uint8_t num = 7; num <= 15; num++) {
        if (!addr_taken(dir | num)) {
            return dir | num;
        }
    }
    return 0;
}

bool clone_build(const uint8_t *dev, const uint8_t *cfg, uint16_t cfg_len,
                 uint8_t speed, char *err, size_t errlen) {
    static const uint8_t cdc_tmpl[] = {
        TUD_CDC_DESCRIPTOR(0, STRIDX_CDC_ITF, CDC_EP_NOTIF, 16, CDC_EP_OUT, CDC_EP_IN, 64)};

    clone_clear();
    if (cfg_len < 9 || cfg_len + sizeof(cdc_tmpl) > CLONE_CFG_MAX) {
        seterr(err, errlen, "configuration descriptor too large");
        return false;
    }
    memcpy(g_clone.dev_desc, dev, 18);
    g_clone.speed = speed;
    g_clone.vid = dev[8] | (dev[9] << 8);
    g_clone.pid = dev[10] | (dev[11] << 8);
    g_clone.num_itf = cfg[4];
    g_clone.cfg_value = cfg[5];

    // Layout: configuration header, our CDC-ACM function, then the device's
    // own interfaces. The CDC function keeps the highest interface numbers
    // (the device's stay what its driver expects) but comes first in the
    // descriptor: hosts that probe interfaces in descriptor order then bind
    // our class driver before the device's driver starts asking questions
    // that can only be answered over the relay channel.
    if (cfg[0] != 9) {
        seterr(err, errlen, "malformed configuration descriptor");
        return false;
    }
    g_clone.clone_off = 9 + sizeof(cdc_tmpl);
    g_clone.cfg_len = cfg_len + sizeof(cdc_tmpl);
    memcpy(g_clone.cfg, cfg, 9);
    memcpy(g_clone.cfg + g_clone.clone_off, cfg + 9, cfg_len - 9);

    uint8_t *p = g_clone.cfg + g_clone.clone_off;
    uint8_t *end = g_clone.cfg + g_clone.cfg_len;
    uint8_t cur_itf = 0, cur_alt = 0;
    bool cur_bot = false;
    while (p + 2 <= end && p[0] >= 2 && p + p[0] <= end) {
        if (p[1] == TUSB_DESC_INTERFACE && p[0] >= 9) {
            cur_itf = p[2];
            cur_alt = p[3];
            cur_bot = p[5] == TUSB_CLASS_MSC && p[7] == 0x50;
        } else if (p[1] == TUSB_DESC_ENDPOINT && p[0] >= 7) {
            uint8_t raddr = p[2];
            uint8_t type = p[3] & 0x03;
            uint16_t real_mps = (p[4] | (p[5] << 8)) & 0x07FF;
            if (type == TUSB_XFER_CONTROL) {
                seterr(err, errlen, "unexpected control endpoint descriptor");
                return false;
            }
            clone_ep_t *e = ep_by_raddr(raddr);
            if (!e) {
                if (g_clone.n_ep == CLONE_EP_MAX) {
                    seterr(err, errlen, "too many endpoints");
                    return false;
                }
                uint8_t gaddr = type == TUSB_XFER_ISOCHRONOUS ? 0 : alloc_gaddr(raddr);
                bool phantom = !gaddr;
                bool silent = false;
                if (phantom) {
                    // Cannot be backed: it stays in the descriptors, so the
                    // device still looks like itself, but nothing answers on
                    // it. In an alternate setting the host has to select
                    // first, the selection is refused. In the default setting
                    // the interface it belongs to is dead; endpoints are
                    // handed out in descriptor order, so that is the device's
                    // later interfaces, not its first.
                    g_clone.n_phantom++;
                    if (cur_alt == 0) {
                        ESP_LOGW(TAG, "endpoint %02x of the default setting is not backed (%s)",
                                 raddr,
                                 type == TUSB_XFER_ISOCHRONOUS ? "isochronous" : "none left");
                    }
                    gaddr = alloc_phantom_addr(raddr, type != TUSB_XFER_ISOCHRONOUS, &silent);
                    if (!gaddr) {
                        seterr(err, errlen, "too many endpoints");
                        return false;
                    }
                }
                e = &g_clone.ep[g_clone.n_ep++];
                e->raddr = raddr;
                e->gaddr = gaddr;
                e->type = type;
                e->phantom = phantom;
                e->silent = silent;
                // We are a full speed device whatever the original was.
                e->mps = (real_mps > 64 && type != TUSB_XFER_ISOCHRONOUS) ? 64 : real_mps;
                e->interval = p[6];
                if (speed >= USBIP_SPEED_HIGH && type == TUSB_XFER_INTERRUPT) {
                    // High speed bInterval is 2^(n-1) microframes; ours is ms.
                    uint8_t n = p[6] ? p[6] : 1;
                    uint32_t ms = (1u << (n > 16 ? 15 : n - 1)) / 8;
                    e->interval = ms < 1 ? 1 : (ms > 255 ? 255 : ms);
                }
                e->bot = cur_bot && type == TUSB_XFER_BULK && !phantom;
                if (e->bot) {
                    e->urb_len = BOT_CHUNK;
                } else if (raddr & 0x80) {
                    e->urb_len = type == TUSB_XFER_BULK ? BULK_URB_LEN : real_mps;
                } else {
                    uint16_t want = type == TUSB_XFER_BULK ? 512 : real_mps;
                    e->urb_len = e->mps ? (want + e->mps - 1) / e->mps * e->mps : 0;
                }
                if (e->urb_len == 0) {
                    e->urb_len = e->mps ? e->mps : 8;
                }
                if (!phantom) {
                    e->buf = malloc(e->urb_len);
                    e->hold = (raddr & 0x80) ? malloc(e->urb_len) : NULL;
                    if (!e->buf || ((raddr & 0x80) && !e->hold)) {
                        seterr(err, errlen, "out of memory");
                        return false;
                    }
                }
            }
            e->itf = cur_itf;
            e->alt_mask |= 1u << (cur_alt > 7 ? 7 : cur_alt);
            p[2] = e->gaddr;
            p[4] = e->mps & 0xFF;
            p[5] = e->mps >> 8;
            if (type == TUSB_XFER_INTERRUPT) {
                p[6] = e->interval;
            }
        }
        p += p[0];
    }
    if (p != end) {
        seterr(err, errlen, "malformed configuration descriptor");
        return false;
    }

    // TUD_CDC_DESCRIPTOR layout (66 bytes): IAD(8) + control itf(9) +
    // header(5) + call-mgmt(5) + ACM(4) + union(5) + notif ep(7) + data
    // itf(9) + OUT ep(7) + IN ep(7). Every place an interface number is
    // baked in gets patched the same way the NCM template used to be.
    uint8_t *cdc = g_clone.cfg + 9;
    memcpy(cdc, cdc_tmpl, sizeof(cdc_tmpl));
    cdc[2] = g_clone.num_itf;             // IAD bFirstInterface
    cdc[8 + 2] = g_clone.num_itf;         // control interface number
    cdc[8 + 9 + 5 + 4] = g_clone.num_itf + 1;          // call-mgmt bDataInterface
    cdc[8 + 9 + 5 + 5 + 4 + 3] = g_clone.num_itf;      // union bMasterInterface
    cdc[8 + 9 + 5 + 5 + 4 + 4] = g_clone.num_itf + 1;  // union bSlaveInterface0
    cdc[8 + 9 + 5 + 5 + 4 + 5 + 7 + 2] = g_clone.num_itf + 1;  // data interface number
    g_clone.cfg[2] = g_clone.cfg_len & 0xFF;
    g_clone.cfg[3] = g_clone.cfg_len >> 8;
    g_clone.cfg[4] = g_clone.num_itf + 2;

    if (g_clone.dev_desc[4] != 0) {
        // A class at device level (vendor specific on an Xbox 360 pad) tells
        // the host the whole device belongs to one driver: Linux then ignores
        // class-matched interface drivers, macOS never builds the interfaces
        // at all -- either way our network function would get no driver and
        // there would be no link. Declare what the device has become, a
        // composite one with an interface association; the device's own
        // driver still finds it by VID/PID and by its interfaces.
        g_clone.dev_desc[4] = TUSB_CLASS_MISC;
        g_clone.dev_desc[5] = MISC_SUBCLASS_COMMON;
        g_clone.dev_desc[6] = MISC_PROTOCOL_IAD;
    }
    g_clone.dev_desc[7] = CFG_TUD_ENDPOINT0_SIZE;
    // Not the real device's own serial-string index (it may have none, or a
    // different one each time something else is cloned): the agent finds
    // this port by the dongle's own serial number, which must stay put
    // across every attach no matter what is attached.
    g_clone.dev_desc[16] = STRIDX_DONGLE_SERIAL;
    g_clone.dev_desc[17] = 1;
    return true;
}

bool clone_add_string(uint8_t idx, const uint8_t *desc, uint16_t len) {
    if (g_clone.n_str == CLONE_STR_MAX || len < 2 || desc[1] != TUSB_DESC_STRING) {
        return false;
    }
    if (desc[0] < len) {
        len = desc[0];
    }
    uint16_t *copy = malloc((len + 1) & ~1);
    if (!copy) {
        return false;
    }
    memcpy(copy, desc, len);
    ((uint8_t *)copy)[0] = len;
    g_clone.str[g_clone.n_str].idx = idx;
    g_clone.str[g_clone.n_str].desc = copy;
    g_clone.n_str++;
    return true;
}

void clone_set_langids(const uint8_t *desc, uint16_t len) {
    if (len < 4 || desc[1] != TUSB_DESC_STRING) {
        return;
    }
    if (desc[0] < len) {
        len = desc[0];
    }
    if (len > sizeof(g_clone.langid_desc)) {
        len = sizeof(g_clone.langid_desc);
    }
    memcpy(g_clone.langid_desc, desc, len);
    ((uint8_t *)g_clone.langid_desc)[0] = len;
}

void clone_set_bos(const uint8_t *desc, uint16_t len) {
    free(g_clone.bos);
    g_clone.bos = malloc(len);
    if (g_clone.bos) {
        memcpy(g_clone.bos, desc, len);
        g_clone.bos_len = len;
    }
}

bool clone_add_feature(uint8_t itf, uint8_t id, const uint8_t *data, uint16_t len) {
    if (g_clone.n_feature == CLONE_FEATURE_MAX) {
        return false;
    }
    uint8_t *copy = malloc(len);
    if (!copy) {
        return false;
    }
    memcpy(copy, data, len);
    g_clone.feature[g_clone.n_feature].itf = itf;
    g_clone.feature[g_clone.n_feature].id = id;
    g_clone.feature[g_clone.n_feature].len = len;
    g_clone.feature[g_clone.n_feature].data = copy;
    g_clone.n_feature++;
    return true;
}

int hid_feature_reports(const uint8_t *desc, uint16_t len, uint8_t *id, uint16_t *size, int max) {
    uint32_t bits[CLONE_FEATURE_MAX] = {0};
    int n = 0;
    uint32_t rsize = 0, rcount = 0;
    uint8_t rid = 0;
    bool uses_ids = false;
    for (uint16_t i = 0; i < len;) {
        uint8_t prefix = desc[i];
        if (prefix == 0xFE) {  // long item
            if (i + 2 >= len) {
                break;
            }
            i += 3 + desc[i + 1];
            continue;
        }
        uint8_t dlen = (prefix & 3) == 3 ? 4 : (prefix & 3);
        if (i + 1 + dlen > len) {
            break;
        }
        uint32_t val = 0;
        for (uint8_t k = 0; k < dlen; k++) {
            val |= (uint32_t)desc[i + 1 + k] << (8 * k);
        }
        switch (prefix & 0xFC) {
        case 0x74:  // Report Size
            rsize = val;
            break;
        case 0x94:  // Report Count
            rcount = val;
            break;
        case 0x84:  // Report ID
            rid = val;
            uses_ids = true;
            break;
        case 0xB0: {  // Feature
            int k = 0;
            while (k < n && id[k] != rid) {
                k++;
            }
            if (k == n) {
                if (n == max || n == CLONE_FEATURE_MAX) {
                    break;
                }
                id[n++] = rid;
            }
            bits[k] += rsize * rcount;
            break;
        }
        default:
            break;
        }
        i += 1 + dlen;
    }
    for (int k = 0; k < n; k++) {
        size[k] = (bits[k] + 7) / 8 + (uses_ids ? 1 : 0);
    }
    return n;
}

bool clone_add_hid_report(uint8_t itf, const uint8_t *desc, uint16_t len) {
    if (g_clone.n_hid == CLONE_HID_MAX) {
        return false;
    }
    uint8_t *copy = malloc(len);
    if (!copy) {
        return false;
    }
    memcpy(copy, desc, len);
    g_clone.hid[g_clone.n_hid].itf = itf;
    g_clone.hid[g_clone.n_hid].len = len;
    g_clone.hid[g_clone.n_hid].desc = copy;
    g_clone.n_hid++;
    return true;
}

int clone_hid_interfaces(uint8_t *itf, uint16_t *len, int max) {
    int n = 0;
    uint8_t cur_itf = 0, cur_alt = 0, cur_class = 0;
    const uint8_t *p = g_clone.cfg + g_clone.clone_off;
    const uint8_t *end = g_clone.cfg + g_clone.cfg_len;
    while (p + 2 <= end && p[0] >= 2 && p + p[0] <= end) {
        if (p[1] == TUSB_DESC_INTERFACE && p[0] >= 9) {
            cur_itf = p[2];
            cur_alt = p[3];
            cur_class = p[5];
        } else if (p[1] == 0x21 && p[0] >= 9 && cur_class == TUSB_CLASS_HID && cur_alt == 0 &&
                   p[6] == 0x22 && n < max) {
            itf[n] = cur_itf;
            len[n] = p[7] | (p[8] << 8);
            n++;
        }
        p += p[0];
    }
    return n;
}

void clone_string_indices(uint8_t *out, size_t max) {
    size_t n = 0;
    uint8_t cand[3 + 1 + 16];
    size_t nc = 0;
    cand[nc++] = g_clone.dev_desc[14];
    cand[nc++] = g_clone.dev_desc[15];
    cand[nc++] = g_clone.dev_desc[16];
    cand[nc++] = g_clone.cfg[6];
    const uint8_t *p = g_clone.cfg + g_clone.clone_off;
    const uint8_t *end = g_clone.cfg + g_clone.cfg_len;
    while (p + 2 <= end && p[0] >= 2 && p + p[0] <= end) {
        if (p[1] == TUSB_DESC_INTERFACE && p[0] >= 9 && nc < sizeof(cand)) {
            cand[nc++] = p[8];
        }
        p += p[0];
    }
    for (size_t i = 0; i < nc && n + 1 < max; i++) {
        bool dup = cand[i] == 0;
        for (size_t j = 0; j < n && !dup; j++) {
            dup = out[j] == cand[i];
        }
        if (!dup) {
            out[n++] = cand[i];
        }
    }
    out[n] = 0;
}

const uint16_t *clone_string(uint8_t idx) {
    if (idx == 0) {
        return ((uint8_t *)g_clone.langid_desc)[0] ? g_clone.langid_desc : NULL;
    }
    for (int i = 0; i < g_clone.n_str; i++) {
        if (g_clone.str[i].idx == idx) {
            return g_clone.str[i].desc;
        }
    }
    return NULL;
}

void clone_clear(void) {
    for (int i = 0; i < g_clone.n_ep; i++) {
        free(g_clone.ep[i].buf);
        free(g_clone.ep[i].hold);
    }
    for (int i = 0; i < g_clone.n_str; i++) {
        free(g_clone.str[i].desc);
    }
    for (int i = 0; i < g_clone.n_hid; i++) {
        free(g_clone.hid[i].desc);
    }
    for (int i = 0; i < g_clone.n_feature; i++) {
        free(g_clone.feature[i].data);
    }
    free(g_clone.bos);
    memset(&g_clone, 0, sizeof(g_clone));
    s_ctl_wait = false;
    s_cfg_sent = false;
}

// ---- endpoints ------------------------------------------------------------

// ---- Bulk-Only Transport (mass storage) ------------------------------------
//
// A device cannot see how many bytes the host asked for on a bulk IN
// endpoint, only that it is being polled, so bulk IN cannot be forwarded as
// transfers the way a host controller driver does it. For mass storage the
// answer is in the protocol: the command block (CBW) says how much data
// follows and in which direction. So this side follows the command cycle and
// asks the exporter for exactly the phases the host is about to read -- one
// URB at a time, the next only after the previous one came back, which is
// also the order the exporter's own cycle tracking expects. The data-in
// phase is one URB for the whole length the command announced, as a host
// controller driver would send it (the USBridge exporter answers a shorter
// one with the whole phase regardless); its reply is streamed to the host.

typedef enum {
    BOT_IDLE,      // waiting for a CBW on the OUT endpoint
    BOT_CBW,       // CBW forwarded, waiting for its RET_SUBMIT
    BOT_DATA_IN,
    BOT_DATA_OUT,
    BOT_CSW,
} bot_state_t;

static struct {
    bot_state_t st;
    uint32_t rem;        // bytes of the data phase still to move
    bool dir_in;
    uint32_t last_out;   // length of the OUT URB outstanding
    uint8_t *acc;        // data-out phase being collected, NULL if in pieces
    uint32_t acc_len, acc_total;
} s_bot;

static clone_ep_t *bot_ep(bool in) {
    for (int i = 0; i < g_clone.n_ep; i++) {
        clone_ep_t *e = &g_clone.ep[i];
        if (e->bot && !!(e->gaddr & 0x80) == in) {
            return e;
        }
    }
    return NULL;
}

static void bot_arm_out(uint32_t len) {
    clone_ep_t *o = bot_ep(false);
    if (!o || !o->open || o->busy || o->halted) {
        return;
    }
    if (len > o->urb_len) {
        len = o->urb_len;
    }
    uint8_t *dst = s_bot.acc ? s_bot.acc + s_bot.acc_len : o->buf;
    if (usbd_edpt_xfer(0, o->gaddr, dst, len)) {
        o->busy = true;
    }
}

static void bot_submit_in(uint32_t len) {
    clone_ep_t *e = bot_ep(true);
    if (!e || e->in_flight) {
        return;
    }
    if (usbip_submit_in(e - g_clone.ep, len)) {
        e->in_flight = true;
    }
}

static void bot_to_csw(void) {
    s_bot.st = BOT_CSW;
    clone_ep_t *e = bot_ep(true);
    if (e && !e->halted) {
        bot_submit_in(13);
    }
    // A halted IN endpoint gets its status read once the host clears it.
}

static void bot_idle(void) {
    s_bot.st = BOT_IDLE;
    free(s_bot.acc);
    s_bot.acc = NULL;
    clone_ep_t *o = bot_ep(false);
    if (o) {
        bot_arm_out(o->urb_len);
    }
}

// The host sent something on the OUT endpoint.
static void bot_out_received(clone_ep_t *o, uint32_t n) {
    if (s_bot.st == BOT_IDLE && n == 31 && memcmp(o->buf, "USBC", 4) == 0) {
        s_bot.rem = tu_le32toh(tu_unaligned_read32(o->buf + 8));
        s_bot.dir_in = o->buf[12] & 0x80;
        s_bot.st = BOT_CBW;
    }
    if (s_bot.st == BOT_DATA_OUT && s_bot.acc) {
        s_bot.acc_len += n;
        if (s_bot.acc_len < s_bot.acc_total) {
            bot_arm_out(s_bot.acc_total - s_bot.acc_len);
            return;
        }
        uint8_t *data = s_bot.acc;
        s_bot.acc = NULL;
        s_bot.last_out = s_bot.acc_total;
        if (!usbip_submit_out_owned(o - g_clone.ep, data, s_bot.acc_total)) {
            bot_idle();
        }
        return;
    }
    s_bot.last_out = n;
    if (!usbip_submit_out(o - g_clone.ep, o->buf, n)) {
        bot_idle();
    }
}

// The exporter answered an OUT URB.
static void bot_out_done(clone_ep_t *o) {
    if (o->out_status != 0) {
        // The device rejected it: stall, the host reads the status (or
        // resets) after clearing.
        o->halted = true;
        usbd_edpt_stall(0, o->gaddr);
        if (s_bot.st == BOT_DATA_OUT) {
            bot_to_csw();
        } else {
            s_bot.st = BOT_IDLE;
        }
        return;
    }
    switch (s_bot.st) {
    case BOT_CBW:
        if (s_bot.rem == 0) {
            bot_to_csw();
        } else if (s_bot.dir_in) {
            s_bot.st = BOT_DATA_IN;
            bot_submit_in(s_bot.rem);
        } else {
            s_bot.st = BOT_DATA_OUT;
            s_bot.acc = s_bot.rem <= BOT_OUT_WHOLE_MAX ? malloc(s_bot.rem) : NULL;
            s_bot.acc_len = 0;
            s_bot.acc_total = s_bot.rem;
            bot_arm_out(s_bot.rem);
        }
        break;
    case BOT_DATA_OUT:
        s_bot.rem -= s_bot.last_out < s_bot.rem ? s_bot.last_out : s_bot.rem;
        if (s_bot.rem) {
            bot_arm_out(s_bot.rem);
        } else {
            bot_to_csw();
        }
        break;
    default:
        bot_idle();
        break;
    }
}

static void ep_in_try_send(clone_ep_t *e);

// The exporter answered an IN URB (data or status).
static void bot_in_done(clone_ep_t *e) {
    if (e->hold_status != 0) {
        e->hold_full = false;
        e->halted = true;
        usbd_edpt_stall(0, e->gaddr);
        if (s_bot.st == BOT_DATA_IN) {
            s_bot.st = BOT_CSW;  // read once the host has cleared the halt
        }
        return;
    }
    if (s_bot.st == BOT_DATA_IN && e->hold_len == 0) {
        // The device ended the data phase with nothing: so do we.
        e->hold_full = false;
        e->zlp = false;
        if (!e->busy && usbd_edpt_xfer(0, e->gaddr, e->buf, 0)) {
            e->busy = true;
        }
        bot_to_csw();
        return;
    }
    e->ready = true;
    ep_in_try_send(e);
}

// A chunk from hold has just been handed to the endpoint; hold is free again.
static void bot_in_sent(clone_ep_t *e, uint16_t n) {
    if (s_bot.st == BOT_DATA_IN) {
        s_bot.rem -= n < s_bot.rem ? n : s_bot.rem;
        if (!e->hold_last) {
            return;  // the reader is already filling hold with the next piece
        }
        // Data phase over. If the device stopped short of what the host
        // expects and on a packet boundary, the host needs a zero-length
        // packet to see the end.
        e->zlp = s_bot.rem && n % e->mps == 0;
        bot_to_csw();
    } else if (s_bot.st == BOT_CSW) {
        bot_idle();
    }
}

static void ep_in_kick(clone_ep_t *e) {
    if (!g_clone.active || !e->open || e->in_flight || e->ready || e->halted || e->bot ||
        !ep_selected(e)) {
        return;
    }
    if ((int32_t)(xTaskGetTickCount() - e->retry_at) < 0 && e->retry_at) {
        return;
    }
    e->retry_at = 0;
    if (usbip_submit_in(e - g_clone.ep, e->urb_len)) {
        e->in_flight = true;
    }
}

static void ep_in_try_send(clone_ep_t *e) {
    if (!e->open || !e->ready || e->busy) {
        return;
    }
    memcpy(e->buf, e->hold, e->hold_len);
    if (usbd_edpt_xfer(0, e->gaddr, e->buf, e->hold_len)) {
        e->busy = true;
        e->ready = false;
        e->hold_full = false;
        // Ask for the next report now; it waits in hold until the host has
        // taken this one.
        if (e->bot) {
            bot_in_sent(e, e->hold_len);
        } else {
            ep_in_kick(e);
        }
    }
}

static void ep_out_arm(clone_ep_t *e) {
    if (!e->open || e->busy || e->halted || e->bot) {
        return;
    }
    if (usbd_edpt_xfer(0, e->gaddr, e->buf, e->urb_len)) {
        e->busy = true;
    }
}

void clone_in_done(void *ep_index) {
    clone_ep_t *e = &g_clone.ep[(uintptr_t)ep_index];
    if (!g_clone.active || !e->in_flight) {
        e->hold_full = false;
        return;
    }
    if (e->bot) {
        if (e->hold_last) {
            e->in_flight = false;
        }
        bot_in_done(e);
        return;
    }
    e->in_flight = false;
    if (e->hold_status == 0) {
        if (e->hold_len > 0 || e->type == TUSB_XFER_BULK) {
            e->ready = true;
            ep_in_try_send(e);
        } else {
            ep_in_kick(e);
        }
    } else if (e->hold_status == EPIPE_STATUS) {
        e->halted = true;
        if (e->open) {
            usbd_edpt_stall(0, e->gaddr);
        }
    } else {
        e->retry_at = xTaskGetTickCount() + pdMS_TO_TICKS(IN_RETRY_MS);
        if (e->retry_at == 0) {
            e->retry_at = 1;
        }
    }
}

void clone_out_sent(void *ep_index) {
    clone_ep_t *e = &g_clone.ep[(uintptr_t)ep_index];
    if (g_clone.active) {
        ep_out_arm(e);
    }
}

void clone_out_done(void *ep_index) {
    clone_ep_t *e = &g_clone.ep[(uintptr_t)ep_index];
    if (g_clone.active && e->bot) {
        bot_out_done(e);
    }
}

void clone_tick(void) {
    if (!g_clone.active) {
        return;
    }
    for (int i = 0; i < g_clone.n_ep; i++) {
        clone_ep_t *e = &g_clone.ep[i];
        if ((e->gaddr & 0x80) && e->retry_at) {
            ep_in_kick(e);
        }
    }
}

// ---- class driver ---------------------------------------------------------

static void clone_drv_init(void) {}

static bool clone_drv_deinit(void) { return true; }

static void clone_drv_reset(uint8_t rhport) {
    (void)rhport;
    for (int i = 0; i < g_clone.n_ep; i++) {
        clone_ep_t *e = &g_clone.ep[i];
        e->open = false;
        e->busy = false;
        e->halted = false;
    }
    s_ctl_wait = false;
    s_cfg_sent = false;
    memset(s_cur_alt, 0, sizeof(s_cur_alt));
    s_bot.st = BOT_IDLE;
}

// How much of the configuration descriptor one open() call claims: the
// interface with all its alternate settings, or the whole group when it is
// introduced by an interface association (TinyUSB binds every interface of
// an association to the driver that takes the first).
static uint16_t claim_len(const uint8_t *itf) {
    const uint8_t *clone_end = g_clone.cfg + g_clone.cfg_len;
    uint8_t first = itf[2];
    uint8_t count = 1;
    const uint8_t *p = g_clone.cfg + g_clone.clone_off;
    const uint8_t *prev = NULL;
    while (p < itf) {
        prev = p;
        p += p[0];
    }
    if (prev && prev[1] == TUSB_DESC_INTERFACE_ASSOCIATION && prev[0] >= 8) {
        count = prev[3];
    }
    p = itf + itf[0];
    while (p + 2 <= clone_end && p[0] >= 2) {
        if (p[1] == TUSB_DESC_INTERFACE_ASSOCIATION) {
            break;
        }
        if (p[1] == TUSB_DESC_INTERFACE && p[2] >= first + count) {
            break;
        }
        p += p[0];
    }
    if (p > clone_end) {
        p = clone_end;
    }
    return p - itf;
}

static uint16_t clone_drv_open(uint8_t rhport, tusb_desc_interface_t const *desc_itf,
                               uint16_t max_len) {
    const uint8_t *itf = (const uint8_t *)desc_itf;
    if (!g_clone.active || itf < g_clone.cfg + g_clone.clone_off ||
        itf >= g_clone.cfg + g_clone.cfg_len) {
        return 0;
    }
    uint16_t len = claim_len(itf);
    if (len > max_len) {
        return 0;
    }
    // Every backed endpoint is opened here, alternate settings included: this
    // USB core cannot close an endpoint and give its FIFO back, so there is
    // no opening them on SET_INTERFACE. The host only uses the ones of the
    // setting it selected.
    for (const uint8_t *p = itf; p < itf + len; p += p[0]) {
        if (p[1] == TUSB_DESC_ENDPOINT) {
            clone_ep_t *e = ep_by_gaddr(p[2]);
            if (e && e->silent) {
                // Active, never enabled: the core NAKs every IN token. Done
                // on the registers directly -- TinyUSB would want a TX FIFO
                // for it, and there is none for this endpoint number.
                DIEPCTL(e->gaddr & 0x0F) = (e->mps & 0x7FF) | DIEPCTL_USBAEP |
                                           ((uint32_t)e->type << 18) | DIEPCTL_SNAK |
                                           DIEPCTL_SD0PID;
            }
            if (e && (e->phantom || e->open)) {
                continue;
            }
            if (!e || !usbd_edpt_open(rhport, (tusb_desc_endpoint_t const *)p)) {
                ESP_LOGE(TAG, "open endpoint %02x failed", p[2]);
                return 0;
            }
            e->open = true;
            e->busy = false;
            e->halted = false;
            if (e->bot) {
                e->ready = false;
                e->zlp = false;
                if (!(e->gaddr & 0x80)) {
                    bot_idle();
                }
            } else if (e->gaddr & 0x80) {
                e->retry_at = 0;
                ep_in_try_send(e);
                ep_in_kick(e);
            } else {
                ep_out_arm(e);
            }
        }
    }
    if (!s_cfg_sent) {
        // TinyUSB answers SET_CONFIGURATION itself; the exported device still
        // has to hear about it.
        tusb_control_request_t req = {
            .bmRequestType = 0x00,
            .bRequest = TUSB_REQ_SET_CONFIGURATION,
            .wValue = g_clone.cfg_value,
            .wIndex = 0,
            .wLength = 0,
        };
        usbip_submit_ctrl(&req, NULL, 0, 0, false);
        s_cfg_sent = true;
    }
    return len;
}

// Whether alternate setting `alt` of interface `itf` needs an endpoint we
// could not back.
static bool alt_has_phantom(uint8_t itf, uint8_t alt) {
    bool in_alt = false;
    const uint8_t *end = g_clone.cfg + g_clone.cfg_len;
    for (const uint8_t *p = g_clone.cfg + g_clone.clone_off; p < end; p += p[0]) {
        if (p[1] == TUSB_DESC_INTERFACE) {
            in_alt = p[2] == itf && p[3] == alt;
        } else if (p[1] == TUSB_DESC_ENDPOINT && in_alt) {
            clone_ep_t *e = ep_by_gaddr(p[2]);
            if (e && e->phantom) {
                return true;
            }
        }
    }
    return false;
}

static bool clone_drv_control(uint8_t rhport, uint8_t stage,
                              tusb_control_request_t const *req) {
    if (!g_clone.active) {
        return false;
    }
    bool in = req->bmRequestType_bit.direction == TUSB_DIR_IN;
    tusb_control_request_t fwd = *req;
    clone_ep_t *e = NULL;
    if (req->bmRequestType_bit.recipient == TUSB_REQ_RCPT_ENDPOINT) {
        e = ep_by_gaddr(tu_u16_low(req->wIndex));
        if (e) {
            fwd.wIndex = e->raddr;
        }
    }

    if (stage == CONTROL_STAGE_SETUP) {
        if (s_ctl_wait) {
            usbip_cancel_ctrl();
            s_ctl_wait = false;
        }
        s_gen++;
        s_req = *req;

        if (req->bmRequestType_bit.recipient == TUSB_REQ_RCPT_ENDPOINT &&
            req->bmRequestType_bit.type == TUSB_REQ_TYPE_STANDARD) {
            // TinyUSB has already done the (un)stall and sends the status
            // stage itself; pass the request on and restart the endpoint.
            usbip_submit_ctrl(&fwd, NULL, 0, 0, false);
            if (e && req->bRequest == TUSB_REQ_CLEAR_FEATURE &&
                req->wValue == TUSB_REQ_FEATURE_EDPT_HALT) {
                e->halted = false;
                e->busy = false;
                if (e->bot) {
                    if (!(e->gaddr & 0x80)) {
                        if (s_bot.st == BOT_IDLE) {
                            bot_idle();
                        }
                    } else if (s_bot.st == BOT_CSW && !e->ready) {
                        bot_submit_in(13);
                    }
                } else if (e->gaddr & 0x80) {
                    ep_in_try_send(e);
                    ep_in_kick(e);
                } else {
                    ep_out_arm(e);
                }
            }
            return true;
        }
        if (req->bmRequestType == 0x81 && req->bRequest == TUSB_REQ_GET_DESCRIPTOR &&
            tu_u16_high(req->wValue) == 0x22) {
            // Read at attach: the host asks for it while it is still setting
            // the device up, possibly before the link is back.
            for (int i = 0; i < g_clone.n_hid; i++) {
                if (g_clone.hid[i].itf == tu_u16_low(req->wIndex)) {
                    return tud_control_xfer(rhport, req, g_clone.hid[i].desc, g_clone.hid[i].len);
                }
            }
        }
        if (req->bmRequestType == 0x01 && req->bRequest == TUSB_REQ_SET_INTERFACE &&
            tu_u16_low(req->wValue) != 0 &&
            alt_has_phantom(tu_u16_low(req->wIndex), tu_u16_low(req->wValue))) {
            ESP_LOGW(TAG, "refusing alternate setting %u of interface %u (needs endpoints "
                          "this dongle cannot provide)",
                     req->wValue, req->wIndex);
            return false;
        }
        if (req->bmRequestType == 0x01 && req->bRequest == TUSB_REQ_SET_INTERFACE &&
            tu_u16_low(req->wIndex) < CLONE_ITF_MAX) {
            uint8_t itf = tu_u16_low(req->wIndex);
            s_cur_alt[itf] = tu_u16_low(req->wValue);
            for (int i = 0; i < g_clone.n_ep; i++) {
                clone_ep_t *x = &g_clone.ep[i];
                if (x->itf == itf && (x->gaddr & 0x80)) {
                    ep_in_kick(x);
                }
            }
        }
        if (req->bmRequestType == 0x21 && req->bRequest == 0xFF && bot_ep(false)) {
            // Bulk-Only Mass Storage Reset: back to waiting for a command
            // (the endpoint halts are cleared by the host right after).
            clone_ep_t *bi = bot_ep(true);
            if (bi) {
                bi->ready = false;
                bi->zlp = false;
            }
            bot_idle();
        }
        if (in && !mux_connected()) {
            // No way to ask the device yet (see the next case). A feature
            // report read at attach is answered from that copy; anything else
            // is refused at once rather than left hanging until the host's
            // timeout -- a host that is still setting the device up will not
            // bring the link up until this request is out of the way.
            if (req->bmRequestType == 0xA1 && req->bRequest == 0x01 &&
                tu_u16_high(req->wValue) == 3) {
                for (int i = 0; i < g_clone.n_feature; i++) {
                    if (g_clone.feature[i].itf == tu_u16_low(req->wIndex) &&
                        g_clone.feature[i].id == tu_u16_low(req->wValue)) {
                        return tud_control_xfer(rhport, req, g_clone.feature[i].data,
                                                g_clone.feature[i].len);
                    }
                }
            }
            return false;
        }
        if (!in && req->wLength > 0) {
            if (req->wLength > sizeof(s_ctl_buf)) {
                return false;
            }
            return tud_control_xfer(rhport, req, s_ctl_buf, req->wLength);
        }
        if (!in && !mux_connected()) {
            // The host configures the device's interfaces before (or while)
            // it brings our link back up, and would sit out its full control
            // timeout on a request like SET_IDLE that cannot be forwarded
            // yet. Accept it now; it goes to the device once the link is up.
            usbip_submit_ctrl(&fwd, NULL, 0, 0, false);
            return tud_control_status(rhport, req);
        }
        // Answered from clone_ctrl_done once the exporter replies; EP0 NAKs
        // until then.
        if (!usbip_submit_ctrl(&fwd, NULL, 0, s_gen, true)) {
            return false;
        }
        s_ctl_wait = true;
        return true;
    }
    if (stage == CONTROL_STAGE_DATA && !in) {
        // The status stage follows as soon as we return, so the device's
        // verdict on the data cannot reach the host.
        usbip_submit_ctrl(&fwd, s_ctl_buf, req->wLength, 0, false);
    }
    return true;
}

void clone_ctrl_done(void *reply) {
    ctrl_reply_t r = *(ctrl_reply_t *)reply;
    free(reply);
    if (!g_clone.active || !s_ctl_wait || r.gen != s_gen) {
        return;
    }
    s_ctl_wait = false;
    if (r.status != 0) {
        usbd_edpt_stall(0, 0x00);
        usbd_edpt_stall(0, 0x80);
    } else if (s_req.wLength > 0) {
        tud_control_xfer(0, &s_req, s_ctl_buf, r.len);
    } else {
        tud_control_status(0, &s_req);
    }
}

// Maintenance requests (config.h): the way in when the link is down.
static bool maint_request(uint8_t rhport, uint8_t stage, tusb_control_request_t const *req) {
    static char text[MAINT_CHUNK];
    static char snap[8192];
    static size_t snap_len;
    if (stage != CONTROL_STAGE_SETUP) {
        return true;
    }
    bool in = req->bmRequestType & 0x80;
    if (in && req->bRequest == 'S') {
        ctrl_status_line(text, sizeof(text));
        return tud_control_xfer(rhport, req, text, strlen(text));
    }
    if (in && req->bRequest == 'L') {
        if (req->wValue == 0) {
            snap_len = logbuf_copy(snap, sizeof(snap));
        }
        size_t off = (size_t)req->wValue * MAINT_CHUNK;
        size_t n = off < snap_len ? snap_len - off : 0;
        if (n > MAINT_CHUNK) {
            n = MAINT_CHUNK;
        }
        return tud_control_xfer(rhport, req, snap + (n ? off : 0), n);
    }
    if (!in && (req->bRequest == 'B' || req->bRequest == 'R')) {
        ctrl_request_reboot(req->bRequest == 'B');
        return tud_control_status(rhport, req);
    }
    return false;
}

bool tud_vendor_control_xfer_cb(uint8_t rhport, uint8_t stage,
                                tusb_control_request_t const *req) {
    if (req->wIndex == MAINT_WINDEX && req->bmRequestType_bit.recipient == TUSB_REQ_RCPT_DEVICE) {
        return maint_request(rhport, stage, req);
    }
    return clone_drv_control(rhport, stage, req);
}

static bool clone_drv_xfer_cb(uint8_t rhport, uint8_t ep_addr, xfer_result_t result,
                              uint32_t xferred) {
    (void)rhport;
    clone_ep_t *e = ep_by_gaddr(ep_addr);
    if (!e) {
        return false;
    }
    e->busy = false;
    if (e->bot) {
        if (!(ep_addr & 0x80)) {
            if (result == XFER_RESULT_SUCCESS) {
                bot_out_received(e, xferred);
            }
        } else if (e->zlp) {
            e->zlp = false;
            if (usbd_edpt_xfer(0, e->gaddr, e->buf, 0)) {
                e->busy = true;
            }
        } else {
            ep_in_try_send(e);
        }
        return true;
    }
    if (ep_addr & 0x80) {
        ep_in_try_send(e);
        ep_in_kick(e);
    } else if (result != XFER_RESULT_SUCCESS || xferred == 0 ||
               !usbip_submit_out(e - g_clone.ep, e->buf, xferred)) {
        ep_out_arm(e);
    }
    // Otherwise the endpoint is re-armed by clone_out_sent once the data is
    // on the wire, so the host cannot outrun the network.
    return true;
}

static const usbd_class_driver_t s_driver = {
    .name = "CLONE",
    .init = clone_drv_init,
    .deinit = clone_drv_deinit,
    .reset = clone_drv_reset,
    .open = clone_drv_open,
    .control_xfer_cb = clone_drv_control,
    .xfer_cb = clone_drv_xfer_cb,
    .xfer_isr = NULL,
    .sof = NULL,
};

usbd_class_driver_t const *usbd_app_driver_get_cb(uint8_t *driver_count) {
    *driver_count = 1;
    return &s_driver;
}
