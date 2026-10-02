// USB device bring-up, the TinyUSB task and the descriptors: the dongle's
// own (network only) while idle, the cloned device's plus the network
// function while attached.

#include <stdio.h>
#include <string.h>

#include "esp_log.h"
#include "esp_mac.h"
#include "esp_private/usb_phy.h"
#include "freertos/FreeRTOS.h"
#include "freertos/semphr.h"
#include "freertos/task.h"

#include "device/usbd_pvt.h"
#include "dongle.h"

static const char *TAG = "usb";

// How long we stay off the bus so the host notices the unplug.
#define REENUM_OFF_MS 150

static uint8_t s_base_mac[6];
static usb_phy_handle_t s_phy;

static const tusb_desc_device_t s_idle_dev = {
    .bLength = sizeof(tusb_desc_device_t),
    .bDescriptorType = TUSB_DESC_DEVICE,
    .bcdUSB = 0x0200,
    .bDeviceClass = TUSB_CLASS_MISC,
    .bDeviceSubClass = MISC_SUBCLASS_COMMON,
    .bDeviceProtocol = MISC_PROTOCOL_IAD,
    .bMaxPacketSize0 = CFG_TUD_ENDPOINT0_SIZE,
    .idVendor = DONGLE_IDLE_VID,
    .idProduct = DONGLE_IDLE_PID,
    .bcdDevice = 0x0100,
    .iManufacturer = 1,
    .iProduct = 2,
    .iSerialNumber = STRIDX_DONGLE_SERIAL,
    .bNumConfigurations = 1,
};

static const uint8_t s_idle_cfg[] = {
    TUD_CONFIG_DESCRIPTOR(1, 2, 0, TUD_CONFIG_DESC_LEN + TUD_CDC_DESC_LEN, 0, 100),
    TUD_CDC_DESCRIPTOR(0, STRIDX_CDC_ITF, CDC_EP_NOTIF, 16, CDC_EP_OUT, CDC_EP_IN, 64),
};

const uint8_t *usb_core_base_mac(void) { return s_base_mac; }

uint8_t const *tud_descriptor_device_cb(void) {
    return g_clone.active ? g_clone.dev_desc : (uint8_t const *)&s_idle_dev;
}

uint8_t const *tud_descriptor_configuration_cb(uint8_t index) {
    (void)index;
    return g_clone.active ? g_clone.cfg : s_idle_cfg;
}

uint8_t const *tud_descriptor_bos_cb(void) {
    return g_clone.active ? g_clone.bos : NULL;
}

static const uint16_t *ascii_string(const char *s) {
    static uint16_t desc[33];
    size_t n = strlen(s);
    if (n > 32) {
        n = 32;
    }
    for (size_t i = 0; i < n; i++) {
        desc[1 + i] = s[i];
    }
    desc[0] = (TUSB_DESC_STRING << 8) | (2 * n + 2);
    return desc;
}

uint16_t const *tud_descriptor_string_cb(uint8_t index, uint16_t langid) {
    (void)langid;
    char text[16];
    static const uint16_t lang[] = {(TUSB_DESC_STRING << 8) | 4, 0x0409};
    if (index == STRIDX_CDC_ITF) {
        return ascii_string("USBridge control");
    }
    if (index == STRIDX_DONGLE_SERIAL) {
        // Always ours, in both idle and clone mode: clone_build() forces the
        // device descriptor's iSerialNumber to this index instead of leaving
        // whatever the cloned device's own was, so the agent can keep
        // finding this port by the dongle's own serial regardless of what is
        // attached.
        snprintf(text, sizeof(text), "%02X%02X%02X%02X%02X%02X", s_base_mac[0], s_base_mac[1],
                 s_base_mac[2], s_base_mac[3], s_base_mac[4], s_base_mac[5]);
        return ascii_string(text);
    }
    if (g_clone.active) {
        const uint16_t *s = clone_string(index);
        // A device without any strings has no language table either, but the
        // host needs one to read our strings.
        return (!s && index == 0) ? lang : s;
    }
    switch (index) {
    case 0:
        return lang;
    case 1:
        return ascii_string("USBridge");
    case 2:
        return ascii_string("USBridge USB/IP Dongle");
    default:
        return NULL;
    }
}

static void usb_task(void *arg) {
    (void)arg;
    for (;;) {
        // The short timeout doubles as the retry tick for IN endpoints
        // waiting out an error.
        tud_task_ext(2, false);
        clone_tick();
    }
}

typedef struct {
    void (*fn)(void *);
    void *arg;
    SemaphoreHandle_t done;
} sync_call_t;

static void sync_trampoline(void *p) {
    sync_call_t *c = p;
    c->fn(c->arg);
    xSemaphoreGive(c->done);
}

bool usb_call_sync(void (*fn)(void *), void *arg) {
    sync_call_t c = {.fn = fn, .arg = arg, .done = xSemaphoreCreateBinary()};
    if (!c.done) {
        return false;
    }
    usbd_defer_func(sync_trampoline, &c, false);
    xSemaphoreTake(c.done, portMAX_DELAY);
    vSemaphoreDelete(c.done);
    return true;
}

void usb_core_reenumerate(void (*swap)(void *), void *arg) {
    tud_disconnect();
    vTaskDelay(pdMS_TO_TICKS(REENUM_OFF_MS));
    if (swap) {
        usb_call_sync(swap, arg);
    }
    tud_connect();
    ESP_LOGI(TAG, "re-enumerating as %s", g_clone.active ? "clone + control" : "control only");
}

void usb_core_start(void) {
    esp_read_mac(s_base_mac, ESP_MAC_WIFI_STA);

    usb_phy_config_t phy = {
        .controller = USB_PHY_CTRL_OTG,
        .target = USB_PHY_TARGET_INT,
        .otg_mode = USB_OTG_MODE_DEVICE,
        .otg_speed = USB_PHY_SPEED_FULL,
    };
    ESP_ERROR_CHECK(usb_new_phy(&phy, &s_phy));

    tusb_rhport_init_t init = {.role = TUSB_ROLE_DEVICE, .speed = TUSB_SPEED_FULL};
    if (!tusb_init(0, &init)) {
        ESP_LOGE(TAG, "tusb_init failed");
        return;
    }
    xTaskCreatePinnedToCore(usb_task, "usb", 6144, NULL, 10, NULL, 0);
}
