#include <stdarg.h>
#include <stdio.h>
#include <string.h>

#include "esp_log.h"
#include "freertos/FreeRTOS.h"

#include "dongle.h"

#define LOGBUF_SZ 8192

static char s_buf[LOGBUF_SZ];
static size_t s_head;   // next write position
static bool s_wrapped;
static portMUX_TYPE s_mux = portMUX_INITIALIZER_UNLOCKED;
static vprintf_like_t s_prev;

static int log_vprintf(const char *fmt, va_list ap) {
    char line[200];
    va_list copy;
    va_copy(copy, ap);
    int n = vsnprintf(line, sizeof(line), fmt, copy);
    va_end(copy);
    if (n > (int)sizeof(line) - 1) {
        n = sizeof(line) - 1;
    }
    portENTER_CRITICAL(&s_mux);
    for (int i = 0; i < n; i++) {
        s_buf[s_head++] = line[i];
        if (s_head == LOGBUF_SZ) {
            s_head = 0;
            s_wrapped = true;
        }
    }
    portEXIT_CRITICAL(&s_mux);
    return s_prev ? s_prev(fmt, ap) : n;
}

void logbuf_init(void) {
    s_prev = esp_log_set_vprintf(log_vprintf);
}

size_t logbuf_copy(char *dst, size_t max) {
    size_t n = 0;
    portENTER_CRITICAL(&s_mux);
    size_t start = s_wrapped ? s_head : 0;
    size_t total = s_wrapped ? LOGBUF_SZ : s_head;
    if (total > max) {
        start = (start + (total - max)) % LOGBUF_SZ;
        total = max;
    }
    for (; n < total; n++) {
        dst[n] = s_buf[(start + n) % LOGBUF_SZ];
    }
    portEXIT_CRITICAL(&s_mux);
    return n;
}
