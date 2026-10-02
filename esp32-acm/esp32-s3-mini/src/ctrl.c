// Control channel (mux.h channel 0, over the one CDC-ACM pipe): how the
// agent finds the dongle and tells it what to import. One request message
// per frame, one reply message:
//
//   HELLO | STATUS       -> OK usbridge-dongle proto=1 fw=... state=...
//   ATTACH <busid>       -> OK vid=.... pid=.... itfs=N eps=N dead_eps=N
//                         | ERR <reason>
//   DETACH               -> OK
//   LOG                  -> OK, followed by one more frame: the firmware log
//   BOOTLOADER           -> OK, then reboots into the ROM download mode
//   REBOOT               -> OK, then reboots
//
// ATTACH expects the host to have the relay channel already bridged to the
// real exporter (dongle.rs connects there, then sends ATTACH; if it cannot
// reach the exporter it never sends ATTACH at all and reports the error
// itself). ATTACH and DETACH answer first and re-enumerate afterwards:
// swapping the cloned device in or out takes the whole USB device -- this
// control channel included -- off the bus for a moment.

#include <stdio.h>
#include <stdlib.h>
#include <string.h>

#include "esp_log.h"
#include "esp_system.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"
#include "soc/rtc_cntl_reg.h"
#include "soc/usb_wrap_struct.h"

#include "dongle.h"

static const char *TAG = "ctrl";

// Time for the reply to leave before the link goes down.
#define REPLY_FLUSH_MS 150
// How long the host gets to bring the link back up with a clone attached.
#define LINK_DEAD_MS 10000
#define LINK_CHECK_MS 500

static SemaphoreHandle_t s_lock;       // serializes attach/detach
static SemaphoreHandle_t s_lost;       // given when the session died on its own

static void swap_to_clone(void *arg) {
    (void)arg;
    g_clone.active = true;
}

static void swap_to_idle(void *arg) {
    (void)arg;
    g_clone.active = false;
}

// Caller holds s_lock. Takes the clone off the bus and ends the session.
static void detach_locked(void) {
    if (!g_clone.active && !usbip_running()) {
        return;
    }
    usb_core_reenumerate(swap_to_idle, NULL);
    usbip_stop();
    usb_call_sync((void (*)(void *))clone_clear, NULL);
}

static volatile uint8_t s_reboot;  // 1 reboot, 2 reboot into download mode

void ctrl_request_reboot(bool bootloader) { s_reboot = bootloader ? 2 : 1; }

static void do_reboot(bool bootloader) {
    tud_disconnect();
    vTaskDelay(pdMS_TO_TICKS(300));
    if (bootloader) {
        // The ROM's download mode talks through the USB-Serial-JTAG
        // peripheral. The PHY routing and the pull-up override survive a
        // software reset, so give the PHY back before restarting or the
        // chip sits in download mode with no USB at all.
        USB_WRAP.otg_conf.pad_pull_override = 0;
        CLEAR_PERI_REG_MASK(RTC_CNTL_USB_CONF_REG,
                            RTC_CNTL_SW_HW_USB_PHY_SEL | RTC_CNTL_SW_USB_PHY_SEL);
        REG_WRITE(RTC_CNTL_OPTION1_REG, RTC_CNTL_FORCE_DOWNLOAD_BOOT);
    }
    esp_restart();
}

void ctrl_session_lost(void) { xSemaphoreGive(s_lost); }

static void lost_task(void *arg) {
    (void)arg;
    uint32_t dead_ticks = 0;
    for (;;) {
        bool lost = xSemaphoreTake(s_lost, pdMS_TO_TICKS(LINK_CHECK_MS)) == pdTRUE;
        if (s_reboot) {
            do_reboot(s_reboot == 2);
        }
        // The control channel is the only way to reach us and the only way
        // we hear that the exporter is gone. If the host never opens the
        // port with the clone on the bus (its CDC driver rejected the
        // composite device), nobody could ever detach: go back to being
        // just the control channel.
        if (g_clone.active && !mux_connected()) {
            dead_ticks += LINK_CHECK_MS;
        } else {
            dead_ticks = 0;
        }
        bool no_link = dead_ticks >= LINK_DEAD_MS;
        if (!lost && !no_link) {
            continue;
        }
        xSemaphoreTake(s_lock, portMAX_DELAY);
        if (usbip_running() || g_clone.active) {
            ESP_LOGW(TAG, "%s, unplugging the cloned device",
                     no_link ? "host never brought the link up" : "exporter gone");
            detach_locked();
        }
        dead_ticks = 0;
        xSemaphoreGive(s_lock);
    }
}

static void reply(const char *line) { mux_ctrl_send((const uint8_t *)line, strlen(line)); }

void ctrl_status_line(char *out, size_t n) {
    const uint8_t *mac = usb_core_base_mac();
    int len = snprintf(out, n,
                       "OK usbridge-dongle proto=%d fw=%s serial=%02X%02X%02X%02X%02X%02X "
                       "state=%s usb=%s",
                       DONGLE_PROTO, DONGLE_FW_VERSION, mac[0], mac[1], mac[2], mac[3], mac[4],
                       mac[5], g_clone.active ? "attached" : "idle",
                       tud_mounted() ? "configured" : "unconfigured");
    if (g_clone.active && len < (int)n) {
        uint32_t sub, done;
        usbip_stats(&sub, &done);
        len += snprintf(out + len, n - len, " busid=%s vid=%04x pid=%04x urbs=%lu/%lu",
                        g_clone.busid, g_clone.vid, g_clone.pid, (unsigned long)done,
                        (unsigned long)sub);
    }
    if (len < (int)n) {
        snprintf(out + len, n - len, " heap=%lu/%lu up=%lu",
                 (unsigned long)esp_get_free_heap_size(),
                 (unsigned long)esp_get_minimum_free_heap_size(),
                 (unsigned long)(xTaskGetTickCount() / configTICK_RATE_HZ));
    }
}

static void handle(const char *line) {
    char out[400];
    if (!strcmp(line, "HELLO") || !strcmp(line, "STATUS")) {
        ctrl_status_line(out, sizeof(out));
        reply(out);
    } else if (!strncmp(line, "ATTACH ", 7)) {
        char busid[32];
        if (sscanf(line + 7, "%31s", busid) != 1) {
            reply("ERR usage: ATTACH <busid>");
            return;
        }
        xSemaphoreTake(s_lock, portMAX_DELAY);
        if (g_clone.active || usbip_running()) {
            xSemaphoreGive(s_lock);
            reply("ERR busy: a device is attached, DETACH first");
            return;
        }
        char err[96] = "";
        if (!usbip_attach(busid, err, sizeof(err))) {
            xSemaphoreGive(s_lock);
            snprintf(out, sizeof(out), "ERR %s", err);
            reply(out);
            return;
        }
        snprintf(out, sizeof(out), "OK vid=%04x pid=%04x itfs=%u eps=%u dead_eps=%u",
                 g_clone.vid, g_clone.pid, g_clone.num_itf, g_clone.n_ep - g_clone.n_phantom,
                 g_clone.n_phantom);
        reply(out);
        vTaskDelay(pdMS_TO_TICKS(REPLY_FLUSH_MS));
        usb_core_reenumerate(swap_to_clone, NULL);
        xSemaphoreGive(s_lock);
    } else if (!strcmp(line, "DETACH")) {
        reply("OK");
        vTaskDelay(pdMS_TO_TICKS(REPLY_FLUSH_MS));
        xSemaphoreTake(s_lock, portMAX_DELAY);
        detach_locked();
        xSemaphoreGive(s_lock);
    } else if (!strcmp(line, "LOG")) {
        static char log[8192];
        size_t len = logbuf_copy(log, sizeof(log));
        mux_ctrl_send((const uint8_t *)log, len);
    } else if (!strcmp(line, "BOOTLOADER") || !strcmp(line, "REBOOT")) {
        reply("OK");
        vTaskDelay(pdMS_TO_TICKS(REPLY_FLUSH_MS));
        do_reboot(line[0] == 'B');
    } else {
        reply("ERR unknown command");
    }
}

static void ctrl_task(void *arg) {
    (void)arg;
    static char line[128];
    for (;;) {
        int n = mux_ctrl_recv((uint8_t *)line, sizeof(line) - 1, UINT32_MAX);
        if (n <= 0) {
            continue;
        }
        line[n] = 0;
        handle(line);
    }
}

void ctrl_start(void) {
    s_lock = xSemaphoreCreateMutex();
    s_lost = xSemaphoreCreateBinary();
    mux_init();
    xTaskCreate(ctrl_task, "ctrl", 6144, NULL, 5, NULL);
    xTaskCreate(lost_task, "lost", 4096, NULL, 5, NULL);
}
