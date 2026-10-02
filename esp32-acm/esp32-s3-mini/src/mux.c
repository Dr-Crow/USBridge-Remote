// Frames one CDC-ACM pipe into the two logical channels ctrl.c and usbip.c
// used to get over separate TCP sockets on the CDC-NCM link. See mux.h.

#include <stdlib.h>
#include <string.h>

#include "esp_log.h"
#include "freertos/FreeRTOS.h"
#include "freertos/queue.h"
#include "freertos/semphr.h"
#include "freertos/stream_buffer.h"
#include "freertos/task.h"
#include "tusb.h"

#include "mux.h"

static const char *TAG = "mux";

#define CDC_ITF 0

#define CH_CTRL 0
#define CH_RELAY 1

#define CTRL_MSG_MAX 1024      // generous for a LOG reply; requests are tiny
#define RELAY_SB_BYTES 8192
#define WRITE_TIMEOUT_MS 3000  // matches the old SO_SNDTIMEO

typedef struct {
    uint32_t len;
    uint8_t data[];
} ctrl_msg_t;

static QueueHandle_t s_ctrl_q;
static StreamBufferHandle_t s_relay_sb;
static SemaphoreHandle_t s_tx_lock;
static volatile bool s_connected;
static volatile uint32_t s_relay_gen;  // bumped on disconnect to wake blocked readers

// ---- RX: one task parses the byte stream into frames -----------------------

typedef enum { HDR_CH, HDR_LEN, PAYLOAD } parse_state_t;

static void dispatch_ctrl(uint8_t *data, uint32_t len) {
    ctrl_msg_t *m = malloc(sizeof(*m) + len);
    if (!m) {
        free(data);
        return;
    }
    m->len = len;
    if (len) {
        memcpy(m->data, data, len);
    }
    free(data);
    if (xQueueSend(s_ctrl_q, &m, 0) != pdTRUE) {
        free(m);
    }
}

static void rx_task(void *arg) {
    (void)arg;
    parse_state_t state = HDR_CH;
    uint8_t ch = 0;
    uint8_t len_buf[4];
    int len_got = 0;
    uint32_t payload_len = 0, payload_got = 0;
    uint8_t *payload = NULL;

    uint8_t chunk[256];
    for (;;) {
        bool was_connected = tud_cdc_n_connected(CDC_ITF);
        if (was_connected != s_connected) {
            s_connected = was_connected;
            if (!was_connected) {
                // Not a relay-session abort by itself: a USB-level
                // disconnect (e.g. the ~150 ms gap this device's own attach
                // re-enumeration causes) is exactly the brief blip a TCP
                // socket would silently ride out mid-recv(), and the relay
                // channel needs the same resilience now that there is no
                // socket underneath it. mux_relay_read keeps blocking
                // through this; only mux_relay_abort() (a real logical
                // end -- usbip_stop, or a write failure) ends the session.
                state = HDR_CH;
                len_got = 0;
                payload_got = 0;
                free(payload);
                payload = NULL;
                ESP_LOGW(TAG, "host closed the port");
            } else {
                ESP_LOGI(TAG, "host opened the port");
            }
        }
        uint32_t avail = tud_cdc_n_available(CDC_ITF);
        if (!avail) {
            vTaskDelay(pdMS_TO_TICKS(2));
            continue;
        }
        uint32_t n = tud_cdc_n_read(CDC_ITF, chunk, sizeof(chunk));
        for (uint32_t i = 0; i < n; i++) {
            uint8_t b = chunk[i];
            switch (state) {
            case HDR_CH:
                ch = b;
                len_got = 0;
                state = HDR_LEN;
                break;
            case HDR_LEN:
                len_buf[len_got++] = b;
                if (len_got == 4) {
                    payload_len = ((uint32_t)len_buf[0] << 24) | ((uint32_t)len_buf[1] << 16) |
                                 ((uint32_t)len_buf[2] << 8) | len_buf[3];
                    payload_got = 0;
                    if (ch == CH_CTRL) {
                        if (payload_len > CTRL_MSG_MAX) {
                            ESP_LOGW(TAG, "ctrl frame too large (%lu), resyncing",
                                     (unsigned long)payload_len);
                            state = HDR_CH;
                            break;
                        }
                        payload = payload_len ? malloc(payload_len) : NULL;
                        if (payload_len && !payload) {
                            state = HDR_CH;
                            break;
                        }
                    }
                    state = payload_len ? PAYLOAD : HDR_CH;
                    if (!payload_len && ch == CH_CTRL) {
                        dispatch_ctrl(NULL, 0);
                    }
                }
                break;
            case PAYLOAD:
                if (ch == CH_CTRL) {
                    payload[payload_got++] = b;
                } else {
                    // Relay bytes go straight to the stream buffer; block
                    // briefly if it's full rather than drop anything.
                    xStreamBufferSend(s_relay_sb, &b, 1, pdMS_TO_TICKS(1000));
                    payload_got++;
                }
                if (payload_got == payload_len) {
                    if (ch == CH_CTRL) {
                        dispatch_ctrl(payload, payload_len);
                        payload = NULL;
                    }
                    state = HDR_CH;
                }
                break;
            }
        }
    }
}

// ---- TX: both channels share the one pipe, serialized by s_tx_lock --------

static bool write_all(const uint8_t *buf, uint32_t n) {
    uint32_t sent = 0;
    uint32_t deadline = xTaskGetTickCount() + pdMS_TO_TICKS(WRITE_TIMEOUT_MS);
    while (sent < n) {
        if (!tud_cdc_n_connected(CDC_ITF)) {
            return false;
        }
        uint32_t w = tud_cdc_n_write(CDC_ITF, buf + sent, n - sent);
        sent += w;
        tud_cdc_n_write_flush(CDC_ITF);
        if (sent < n) {
            if (xTaskGetTickCount() >= deadline) {
                return false;
            }
            vTaskDelay(1);
        }
    }
    return true;
}

static bool send_frame(uint8_t ch, const uint8_t *buf, uint32_t len) {
    uint8_t hdr[5] = {ch, len >> 24, len >> 16, len >> 8, len};
    xSemaphoreTake(s_tx_lock, portMAX_DELAY);
    bool ok = write_all(hdr, sizeof(hdr)) && (len == 0 || write_all(buf, len));
    xSemaphoreGive(s_tx_lock);
    return ok;
}

// ---- public API -------------------------------------------------------

void mux_init(void) {
    s_ctrl_q = xQueueCreate(4, sizeof(ctrl_msg_t *));
    s_relay_sb = xStreamBufferCreate(RELAY_SB_BYTES, 1);
    s_tx_lock = xSemaphoreCreateMutex();
    xTaskCreate(rx_task, "mux_rx", 4096, NULL, 6, NULL);
}

bool mux_connected(void) { return s_connected; }

int mux_ctrl_recv(uint8_t *buf, size_t max, uint32_t timeout_ms) {
    ctrl_msg_t *m;
    TickType_t ticks = timeout_ms == UINT32_MAX ? portMAX_DELAY : pdMS_TO_TICKS(timeout_ms);
    if (xQueueReceive(s_ctrl_q, &m, ticks) != pdTRUE) {
        return 0;
    }
    uint32_t n = m->len < max ? m->len : (uint32_t)max;
    memcpy(buf, m->data, n);
    free(m);
    return (int)n;
}

bool mux_ctrl_send(const uint8_t *buf, size_t len) { return send_frame(CH_CTRL, buf, (uint32_t)len); }

bool mux_relay_read(void *buf, size_t n) {
    uint8_t *p = buf;
    uint32_t gen = s_relay_gen;
    size_t got = 0;
    while (got < n) {
        size_t r = xStreamBufferReceive(s_relay_sb, p + got, n - got, pdMS_TO_TICKS(200));
        got += r;
        if (got < n && s_relay_gen != gen) {
            return false;  // port dropped mid-read, like recv() returning 0
        }
    }
    return true;
}

bool mux_relay_write(const void *buf, size_t n) { return send_frame(CH_RELAY, buf, (uint32_t)n); }

void mux_relay_reset(void) {
    xStreamBufferReset(s_relay_sb);
}

void mux_relay_abort(void) {
    s_relay_gen++;
    xStreamBufferReset(s_relay_sb);
}
