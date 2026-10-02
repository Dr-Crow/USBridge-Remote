#include "esp_log.h"

#include "dongle.h"

void app_main(void) {
    logbuf_init();
    ESP_LOGI("main", "USBridge USB/IP dongle fw %s", DONGLE_FW_VERSION);
    usb_core_start();
    ctrl_start();
}
