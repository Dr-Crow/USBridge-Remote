// USB/IP importer: the part a VHCI driver plays on a PC. The host bridges
// the relay channel (mux.h) to the real exporter's TCP connection; this
// module just speaks the USB/IP wire protocol over that channel and turns
// the clone driver's requests into CMD_SUBMIT and the exporter's RET_SUBMIT
// (relayed byte for byte, unchanged, by the host) back into endpoint data.

#include <errno.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "device/usbd_pvt.h"
#include "dongle.h"

static const char *TAG = "usbip";

#define USBIP_VERSION 0x0111
#define OP_REQ_IMPORT 0x8003
#define OP_REP_IMPORT 0x0003
#define CMD_SUBMIT 1
#define CMD_UNLINK 2
#define RET_SUBMIT 3
#define RET_UNLINK 4
#define HDR_LEN 48
#define URB_DIR_IN 0x0200

#define PEND_MAX 16
#define TXQ_LEN 16

typedef enum {
    P_FREE = 0,
    P_CTRL,     // control request the host is waiting on
    P_DISCARD,  // reply is read and dropped
    P_EP,       // endpoint URB
} pend_kind_t;

typedef struct {
    uint32_t seq;
    uint32_t unlink_seq;  // seq of the CMD_UNLINK sent for it, 0 if none
    uint32_t gen;
    uint8_t kind;
    uint8_t ep;
    bool in;
} pend_t;

typedef struct {
    uint8_t hdr[HDR_LEN];
    int8_t out_ep;  // endpoint index to re-arm once written, -1 if none
    uint32_t len;
    uint8_t *ext;   // payload when it is not in data[]; freed after writing
    uint8_t data[];
} tx_msg_t;

static volatile bool s_run;
static uint32_t s_devid;
static uint32_t s_seq;
static pend_t s_pend[PEND_MAX];
static portMUX_TYPE s_mux = portMUX_INITIALIZER_UNLOCKED;
static QueueHandle_t s_txq;
static SemaphoreHandle_t s_rx_done, s_tx_done;
static uint32_t s_n_submit, s_n_complete;

static void put32(uint8_t *p, uint32_t v) {
    p[0] = v >> 24;
    p[1] = v >> 16;
    p[2] = v >> 8;
    p[3] = v;
}

static uint32_t get32(const uint8_t *p) {
    return ((uint32_t)p[0] << 24) | ((uint32_t)p[1] << 16) | ((uint32_t)p[2] << 8) | p[3];
}

// Thin names over the relay channel so the rest of this file reads the same
// as when these were a raw TCP socket's recv()/send() loops.
static bool read_full(void *buf, size_t n) { return mux_relay_read(buf, n); }
static bool write_full(const void *buf, size_t n) { return mux_relay_write(buf, n); }

static bool discard(size_t n) {
    uint8_t scratch[128];
    while (n) {
        size_t chunk = n < sizeof(scratch) ? n : sizeof(scratch);
        if (!read_full(scratch, chunk)) {
            return false;
        }
        n -= chunk;
    }
    return true;
}

static void fill_submit(uint8_t *h, uint32_t seq, bool in, uint8_t epnum, uint32_t len,
                        const uint8_t *setup, uint8_t interval) {
    memset(h, 0, HDR_LEN);
    put32(h, CMD_SUBMIT);
    put32(h + 4, seq);
    put32(h + 8, s_devid);
    put32(h + 12, in ? 1 : 0);
    put32(h + 16, epnum);
    put32(h + 20, in ? URB_DIR_IN : 0);
    put32(h + 24, len);
    put32(h + 36, interval);
    if (setup) {
        memcpy(h + 40, setup, 8);
    }
}

// ---- attach: synchronous part ---------------------------------------------

// One control transfer, request and reply both read inline. Only valid
// before the reader task exists. Returns the reply length or -1.
static int ctrl_sync(uint8_t bm, uint8_t req, uint16_t value, uint16_t index,
                     uint8_t *buf, uint16_t len) {
    uint8_t setup[8] = {bm, req, value & 0xFF, value >> 8, index & 0xFF, index >> 8,
                        len & 0xFF, len >> 8};
    uint8_t h[HDR_LEN];
    uint32_t seq = ++s_seq;
    fill_submit(h, seq, true, 0, len, setup, 0);
    if (!write_full(h, HDR_LEN)) {
        return -1;
    }
    for (;;) {
        if (!read_full(h, HDR_LEN)) {
            return -1;
        }
        uint32_t alen = get32(h + 24);
        if (get32(h) != RET_SUBMIT || alen > 65536) {
            return -1;
        }
        uint32_t take = alen < len ? alen : len;
        if (!read_full(buf, take) || !discard(alen - take)) {
            return -1;
        }
        if (get32(h + 4) != seq) {
            continue;
        }
        return (int32_t)get32(h + 20) == 0 ? (int)take : -1;
    }
}

static bool fail(char *err, size_t errlen, const char *msg) {
    if (msg != err) {
        snprintf(err, errlen, "%s", msg);
    }
    ESP_LOGW(TAG, "attach: %s", err);
    clone_clear();
    return false;
}

static void rx_task(void *arg);
static void tx_task(void *arg);

// The host has already bridged the relay channel to the real exporter's TCP
// connection by the time this is called (and has already handled "cannot
// reach the exporter" itself, over the control channel, without ever
// forwarding an ATTACH here) -- this just speaks the USB/IP wire protocol
// over that channel, same bytes as when it was a raw socket.
bool usbip_attach(const char *busid, char *err, size_t errlen) {
    static uint8_t buf[CLONE_CFG_MAX];
    uint8_t dev[18];

    mux_relay_reset();
    uint8_t req[8 + 32] = {0};
    req[0] = USBIP_VERSION >> 8;
    req[1] = USBIP_VERSION & 0xFF;
    req[2] = OP_REQ_IMPORT >> 8;
    req[3] = OP_REQ_IMPORT & 0xFF;
    strncpy((char *)req + 8, busid, 31);
    uint8_t rep[8];
    if (!write_full(req, sizeof(req)) || !read_full(rep, 8)) {
        return fail(err, errlen, "import: no reply");
    }
    if (((rep[2] << 8) | rep[3]) != OP_REP_IMPORT || get32(rep + 4) != 0) {
        return fail(err, errlen, "exporter refused the bus id");
    }
    // usbip_usb_device: path[256] busid[32] busnum devnum speed ids...
    if (!read_full(buf, 312)) {
        return fail(err, errlen, "import: short reply");
    }
    s_devid = (get32(buf + 288) << 16) | (get32(buf + 292) & 0xFFFF);
    uint8_t speed = get32(buf + 296);

    if (ctrl_sync(0x80, TUSB_REQ_GET_DESCRIPTOR, TUSB_DESC_DEVICE << 8, 0, dev, 18) != 18) {
        return fail(err, errlen, "cannot read the device descriptor");
    }
    if (ctrl_sync(0x80, TUSB_REQ_GET_DESCRIPTOR, TUSB_DESC_CONFIGURATION << 8, 0, buf, 9) != 9) {
        return fail(err, errlen, "cannot read the configuration descriptor");
    }
    uint16_t total = buf[2] | (buf[3] << 8);
    if (total > sizeof(buf)) {
        return fail(err, errlen, "configuration descriptor too large");
    }
    if (ctrl_sync(0x80, TUSB_REQ_GET_DESCRIPTOR, TUSB_DESC_CONFIGURATION << 8, 0, buf, total) !=
        total) {
        return fail(err, errlen, "cannot read the configuration descriptor");
    }
    if (!clone_build(dev, buf, total, speed, err, errlen)) {
        return fail(err, errlen, err);
    }
    strncpy(g_clone.busid, busid, sizeof(g_clone.busid) - 1);

    // String and BOS requests are answered from a callback that cannot wait
    // for the network, so everything the descriptors point at is read now.
    int n = ctrl_sync(0x80, TUSB_REQ_GET_DESCRIPTOR, TUSB_DESC_STRING << 8, 0, buf, 255);
    uint16_t langid = 0x0409;
    if (n >= 4) {
        clone_set_langids(buf, n);
        langid = buf[2] | (buf[3] << 8);
    }
    uint8_t idx[24];
    clone_string_indices(idx, sizeof(idx));
    for (int i = 0; idx[i]; i++) {
        n = ctrl_sync(0x80, TUSB_REQ_GET_DESCRIPTOR, (TUSB_DESC_STRING << 8) | idx[i], langid,
                      buf, 255);
        if (n >= 2) {
            clone_add_string(idx[i], buf, n);
        }
    }
    uint8_t hid_itf[CLONE_HID_MAX];
    uint16_t hid_len[CLONE_HID_MAX];
    int n_hid = clone_hid_interfaces(hid_itf, hid_len, CLONE_HID_MAX);
    for (int i = 0; i < n_hid; i++) {
        if (hid_len[i] == 0 || hid_len[i] > sizeof(buf)) {
            continue;
        }
        n = ctrl_sync(0x81, TUSB_REQ_GET_DESCRIPTOR, 0x22 << 8, hid_itf[i], buf, hid_len[i]);
        if (n <= 0) {
            continue;
        }
        clone_add_hid_report(hid_itf[i], buf, n);
        // Drivers read feature reports while they set the device up, which
        // can be before the host lets our link come up.
        static uint8_t rid[CLONE_FEATURE_MAX];
        static uint16_t rlen[CLONE_FEATURE_MAX];
        static uint8_t rep[512];
        int n_rep = hid_feature_reports(buf, n, rid, rlen, CLONE_FEATURE_MAX);
        for (int k = 0; k < n_rep; k++) {
            if (rlen[k] == 0 || rlen[k] > sizeof(rep)) {
                continue;
            }
            int got = ctrl_sync(0xA1, 0x01, (3 << 8) | rid[k], hid_itf[i], rep, rlen[k]);
            if (got > 0) {
                clone_add_feature(hid_itf[i], rid[k], rep, got);
            }
        }
    }
    ESP_LOGI(TAG, "cached %u HID report descriptor(s), %u feature report(s)", g_clone.n_hid,
             g_clone.n_feature);
    if ((dev[2] | (dev[3] << 8)) >= 0x0201) {
        n = ctrl_sync(0x80, TUSB_REQ_GET_DESCRIPTOR, TUSB_DESC_BOS << 8, 0, buf, 5);
        if (n == 5) {
            total = buf[2] | (buf[3] << 8);
            if (total <= sizeof(buf) &&
                ctrl_sync(0x80, TUSB_REQ_GET_DESCRIPTOR, TUSB_DESC_BOS << 8, 0, buf, total) ==
                    total) {
                clone_set_bos(buf, total);
            }
        }
    }

    if (!s_txq) {
        s_txq = xQueueCreate(TXQ_LEN, sizeof(tx_msg_t *));
        s_rx_done = xSemaphoreCreateBinary();
        s_tx_done = xSemaphoreCreateBinary();
    }
    memset(s_pend, 0, sizeof(s_pend));
    s_n_submit = s_n_complete = 0;
    s_run = true;
    xTaskCreate(rx_task, "usbip_rx", 4096, NULL, 6, NULL);
    xTaskCreate(tx_task, "usbip_tx", 3072, NULL, 6, NULL);
    ESP_LOGI(TAG, "imported %s %04x:%04x speed=%u itfs=%u eps=%u", busid, g_clone.vid,
             g_clone.pid, speed, g_clone.num_itf, g_clone.n_ep);
    return true;
}

void usbip_stop(void) {
    if (!s_run) {
        return;
    }
    s_run = false;
    mux_relay_abort();
    tx_msg_t *stop = NULL;
    xQueueSend(s_txq, &stop, portMAX_DELAY);
    xSemaphoreTake(s_rx_done, portMAX_DELAY);
    xSemaphoreTake(s_tx_done, portMAX_DELAY);
    tx_msg_t *m;
    while (xQueueReceive(s_txq, &m, 0) == pdTRUE) {
        if (m) {
            free(m->ext);
        }
        free(m);
    }
    ESP_LOGI(TAG, "session closed, %lu URBs submitted, %lu completed",
             (unsigned long)s_n_submit, (unsigned long)s_n_complete);
}

bool usbip_running(void) { return s_run; }

void usbip_stats(uint32_t *submitted, uint32_t *completed) {
    *submitted = s_n_submit;
    *completed = s_n_complete;
}

// ---- URB bookkeeping ------------------------------------------------------

static bool pend_add(uint32_t seq, pend_kind_t kind, uint8_t ep, bool in, uint32_t gen) {
    bool ok = false;
    portENTER_CRITICAL(&s_mux);
    for (int i = 0; i < PEND_MAX; i++) {
        if (s_pend[i].kind == P_FREE) {
            s_pend[i] = (pend_t){.seq = seq, .kind = kind, .ep = ep, .in = in, .gen = gen};
            ok = true;
            break;
        }
    }
    portEXIT_CRITICAL(&s_mux);
    return ok;
}

static bool pend_take(uint32_t seq, pend_t *out) {
    bool ok = false;
    portENTER_CRITICAL(&s_mux);
    for (int i = 0; i < PEND_MAX; i++) {
        if (s_pend[i].kind != P_FREE && s_pend[i].seq == seq) {
            *out = s_pend[i];
            s_pend[i].kind = P_FREE;
            ok = true;
            break;
        }
    }
    portEXIT_CRITICAL(&s_mux);
    return ok;
}

static bool post(tx_msg_t *m) {
    if (!s_run || xQueueSend(s_txq, &m, 0) != pdTRUE) {
        free(m->ext);
        free(m);
        return false;
    }
    s_n_submit++;
    return true;
}

bool usbip_submit_ctrl(const tusb_control_request_t *req, const uint8_t *out,
                       uint16_t out_len, uint32_t gen, bool want_reply) {
    if (!s_run) {
        return false;
    }
    bool in = req->bmRequestType & 0x80;
    tx_msg_t *m = malloc(sizeof(*m) + out_len);
    if (!m) {
        return false;
    }
    uint32_t seq = ++s_seq;
    uint32_t len = in ? (req->wLength > CTL_BUF_SZ ? CTL_BUF_SZ : req->wLength) : out_len;
    fill_submit(m->hdr, seq, in, 0, len, (const uint8_t *)req, 0);
    m->out_ep = -1;
    m->ext = NULL;
    m->len = out_len;
    if (out_len) {
        memcpy(m->data, out, out_len);
    }
    if (!pend_add(seq, want_reply ? P_CTRL : P_DISCARD, 0, in, gen)) {
        free(m);
        return false;
    }
    if (!post(m)) {
        pend_t p;
        pend_take(seq, &p);
        return false;
    }
    return true;
}

bool usbip_submit_in(uint8_t ep_index, uint32_t len) {
    if (!s_run) {
        return false;
    }
    clone_ep_t *e = &g_clone.ep[ep_index];
    tx_msg_t *m = malloc(sizeof(*m));
    if (!m) {
        return false;
    }
    uint32_t seq = ++s_seq;
    fill_submit(m->hdr, seq, true, e->raddr & 0x0F, len, NULL, e->interval);
    m->out_ep = -1;
    m->ext = NULL;
    m->len = 0;
    if (!pend_add(seq, P_EP, ep_index, true, 0)) {
        free(m);
        return false;
    }
    if (!post(m)) {
        pend_t p;
        pend_take(seq, &p);
        return false;
    }
    return true;
}

bool usbip_submit_out(uint8_t ep_index, const uint8_t *data, uint16_t len) {
    if (!s_run) {
        return false;
    }
    clone_ep_t *e = &g_clone.ep[ep_index];
    tx_msg_t *m = malloc(sizeof(*m) + len);
    if (!m) {
        return false;
    }
    uint32_t seq = ++s_seq;
    fill_submit(m->hdr, seq, false, e->raddr & 0x0F, len, NULL, e->interval);
    m->out_ep = ep_index;
    m->ext = NULL;
    m->len = len;
    memcpy(m->data, data, len);
    if (!pend_add(seq, P_EP, ep_index, false, 0)) {
        free(m);
        return false;
    }
    if (!post(m)) {
        pend_t p;
        pend_take(seq, &p);
        return false;
    }
    return true;
}

bool usbip_submit_out_owned(uint8_t ep_index, uint8_t *data, uint32_t len) {
    clone_ep_t *e = &g_clone.ep[ep_index];
    tx_msg_t *m = s_run ? malloc(sizeof(*m)) : NULL;
    if (!m) {
        free(data);
        return false;
    }
    uint32_t seq = ++s_seq;
    fill_submit(m->hdr, seq, false, e->raddr & 0x0F, len, NULL, e->interval);
    m->out_ep = ep_index;
    m->len = len;
    m->ext = data;
    if (!pend_add(seq, P_EP, ep_index, false, 0)) {
        free(data);
        free(m);
        return false;
    }
    if (!post(m)) {
        pend_t p;
        pend_take(seq, &p);
        return false;
    }
    return true;
}

void usbip_cancel_ctrl(void) {
    uint32_t target = 0, unlink = 0;
    portENTER_CRITICAL(&s_mux);
    for (int i = 0; i < PEND_MAX; i++) {
        if (s_pend[i].kind == P_CTRL) {
            // Its RET_SUBMIT may already be on the way; whichever of that and
            // the RET_UNLINK comes first frees the entry.
            s_pend[i].kind = P_DISCARD;
            s_pend[i].unlink_seq = unlink = ++s_seq;
            target = s_pend[i].seq;
            break;
        }
    }
    portEXIT_CRITICAL(&s_mux);
    if (!target) {
        return;
    }
    tx_msg_t *m = malloc(sizeof(*m));
    if (!m) {
        return;
    }
    memset(m->hdr, 0, HDR_LEN);
    put32(m->hdr, CMD_UNLINK);
    put32(m->hdr + 4, unlink);
    put32(m->hdr + 8, s_devid);
    put32(m->hdr + 20, target);
    m->out_ep = -1;
    m->ext = NULL;
    m->len = 0;
    post(m);
}

// ---- tasks ----------------------------------------------------------------

static void rx_task(void *arg) {
    (void)arg;
    uint8_t h[HDR_LEN];
    while (s_run) {
        if (!read_full(h, HDR_LEN)) {
            break;
        }
        uint32_t cmd = get32(h);
        uint32_t seq = get32(h + 4);
        int32_t status = (int32_t)get32(h + 20);
        uint32_t alen = get32(h + 24);

        if (cmd == RET_UNLINK) {
            portENTER_CRITICAL(&s_mux);
            for (int i = 0; i < PEND_MAX; i++) {
                if (s_pend[i].kind != P_FREE && s_pend[i].unlink_seq == seq) {
                    s_pend[i].kind = P_FREE;
                }
            }
            portEXIT_CRITICAL(&s_mux);
            continue;
        }
        if (cmd != RET_SUBMIT) {
            ESP_LOGW(TAG, "unexpected command %lu", (unsigned long)cmd);
            break;
        }
        pend_t p;
        if (!pend_take(seq, &p)) {
            continue;
        }
        s_n_complete++;
        uint32_t dlen = p.in ? alen : 0;
        if (dlen > (64u << 20)) {
            break;
        }
        if (p.kind == P_CTRL && p.gen == clone_ctl_gen()) {
            uint32_t take = dlen < CTL_BUF_SZ ? dlen : CTL_BUF_SZ;
            if (!read_full(clone_ctl_buf(), take) || !discard(dlen - take)) {
                break;
            }
            ctrl_reply_t *r = malloc(sizeof(*r));
            if (r) {
                *r = (ctrl_reply_t){.gen = p.gen, .status = status, .len = take};
                usbd_defer_func(clone_ctrl_done, r, false);
            }
        } else if (p.kind == P_EP && p.in && g_clone.ep[p.ep].bot) {
            // A mass-storage data phase comes back as one reply of any size;
            // it goes to the endpoint piece by piece as the host drains it.
            clone_ep_t *e = &g_clone.ep[p.ep];
            bool ok = true;
            do {
                while (e->hold_full && s_run && g_clone.active) {
                    vTaskDelay(1);
                }
                uint32_t take = dlen < e->urb_len ? dlen : e->urb_len;
                if (!s_run || !g_clone.active || !read_full(e->hold, take)) {
                    ok = false;
                    break;
                }
                dlen -= take;
                e->hold_len = take;
                e->hold_status = status;
                e->hold_last = dlen == 0;
                e->hold_full = true;
                usbd_defer_func(clone_in_done, (void *)(uintptr_t)p.ep, false);
            } while (dlen);
            if (!ok) {
                break;
            }
        } else if (p.kind == P_EP && p.in) {
            clone_ep_t *e = &g_clone.ep[p.ep];
            uint32_t take = dlen < e->urb_len ? dlen : e->urb_len;
            if (!read_full(e->hold, take) || !discard(dlen - take)) {
                break;
            }
            e->hold_len = take;
            e->hold_status = status;
            usbd_defer_func(clone_in_done, (void *)(uintptr_t)p.ep, false);
        } else if (p.kind == P_EP) {
            g_clone.ep[p.ep].out_status = status;
            usbd_defer_func(clone_out_done, (void *)(uintptr_t)p.ep, false);
        } else if (!discard(dlen)) {
            break;
        }
    }
    if (s_run) {
        ESP_LOGW(TAG, "connection to the exporter lost");
        ctrl_session_lost();
    }
    xSemaphoreGive(s_rx_done);
    vTaskDelete(NULL);
}

static void tx_task(void *arg) {
    (void)arg;
    for (;;) {
        tx_msg_t *m;
        xQueueReceive(s_txq, &m, portMAX_DELAY);
        if (!m) {
            break;
        }
        // Nothing goes out until the host has the port open and its bridge
        // to the exporter is up: a frame written into that gap is just lost
        // (mux_relay_write's own timeout is far longer than the gap).
        // tud_cdc_n_connected() flips true the instant the USB bus
        // reconfigures, but the host still has to notice its device node
        // went away and reopen the new one -- a write landing in that
        // reopen gap is dropped silently (nothing has the port open yet to
        // buffer it into). Settle for the same ~0.65s the re-enumeration
        // itself is documented to take before trusting a write will land.
        if (s_run && !mux_connected()) {
            while (s_run && !mux_connected()) {
                vTaskDelay(pdMS_TO_TICKS(10));
            }
            vTaskDelay(pdMS_TO_TICKS(700));
        }
        bool ok = write_full(m->hdr, HDR_LEN) &&
                  (m->len == 0 || write_full(m->ext ? m->ext : m->data, m->len));
        free(m->ext);
        if (m->out_ep >= 0) {
            usbd_defer_func(clone_out_sent, (void *)(uintptr_t)m->out_ep, false);
        }
        free(m);
        if (!ok && s_run) {
            // The reader sees the same failure and reports it.
            mux_relay_abort();
        }
    }
    xSemaphoreGive(s_tx_done);
    vTaskDelete(NULL);
}
