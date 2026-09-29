//go:build cgo && (linux || darwin || ios || windows || android)

// Per-frame benchmark recorder -- see bench_frames.h.
//
// Single producer (the thread running dr_submit) and single consumer (the
// Go drain loop in bench_frames_cgo.go), so a lock-free ring with
// acquire/release indices is enough.

#include "bench_frames.h"

#define BENCH_FRAMES_CAP 16384 // ~4.5 min at 60fps; drained every 100ms

static bench_frame_t     g_bench_frames[BENCH_FRAMES_CAP];
static volatile uint32_t g_bench_head    = 0; // written by producer
static volatile uint32_t g_bench_tail    = 0; // written by consumer
static volatile int      g_bench_enabled = 0;
static volatile uint32_t g_bench_dropped = 0; // ring full

void bench_frames_note(const DECODE_UNIT *du) {
    if (!__atomic_load_n(&g_bench_enabled, __ATOMIC_ACQUIRE)) return;
    uint32_t head = __atomic_load_n(&g_bench_head, __ATOMIC_RELAXED);
    uint32_t tail = __atomic_load_n(&g_bench_tail, __ATOMIC_ACQUIRE);
    if (head - tail >= BENCH_FRAMES_CAP) {
        __atomic_fetch_add(&g_bench_dropped, 1, __ATOMIC_RELAXED);
        return;
    }
    bench_frame_t *f = &g_bench_frames[head % BENCH_FRAMES_CAP];
    f->frame_number           = du->frameNumber;
    f->frame_type             = du->frameType;
    f->size_bytes             = (uint32_t)du->fullLength;
    f->host_latency_tenths_ms = du->frameHostProcessingLatency;
    f->receive_us             = du->receiveTimeUs;
    f->enqueue_us             = du->enqueueTimeUs;
    f->submit_us              = LiGetMicroseconds();
    f->presentation_us        = du->presentationTimeUs;
    __atomic_store_n(&g_bench_head, head + 1, __ATOMIC_RELEASE);
}

// bench_frames_enable starts (1) or stops (0) recording. Starting discards
// anything left over, so a run only ever sees its own frames.
void bench_frames_enable(int on) {
    if (on) {
        __atomic_store_n(&g_bench_tail, __atomic_load_n(&g_bench_head, __ATOMIC_ACQUIRE), __ATOMIC_RELEASE);
        __atomic_store_n(&g_bench_dropped, 0, __ATOMIC_RELAXED);
    }
    __atomic_store_n(&g_bench_enabled, on ? 1 : 0, __ATOMIC_RELEASE);
}

// bench_frames_drain copies up to max recorded frames into out and returns
// how many were copied.
int bench_frames_drain(bench_frame_t *out, int max) {
    uint32_t tail = __atomic_load_n(&g_bench_tail, __ATOMIC_RELAXED);
    uint32_t head = __atomic_load_n(&g_bench_head, __ATOMIC_ACQUIRE);
    int n = 0;
    while (tail != head && n < max) {
        out[n++] = g_bench_frames[tail % BENCH_FRAMES_CAP];
        tail++;
    }
    __atomic_store_n(&g_bench_tail, tail, __ATOMIC_RELEASE);
    return n;
}

uint32_t bench_frames_dropped(void) {
    return __atomic_load_n(&g_bench_dropped, __ATOMIC_RELAXED);
}

uint64_t bench_now_us(void) { return LiGetMicroseconds(); }

