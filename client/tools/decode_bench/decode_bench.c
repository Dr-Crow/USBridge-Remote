// decode_bench: compares the Windows client's three decode threading modes
// (direct / thread / pull) with real Vulkan Video decode on this GPU.
// A "network" thread releases one access unit every 1/fps seconds, like
// packets arriving from the host. Latency = scheduled arrival -> the frame's
// timeline semaphore signalled (GPU decode done, ready for the renderer).
//
// usage: decode_bench <clip.hevc> <direct|thread|pull> [fps] [seconds]
//   BENCH_THREADS=n     libavcodec frame threads (FF_THREAD_FRAME)
//   BENCH_NOLOWDELAY=1  drop AV_CODEC_FLAG_LOW_DELAY (frame threading needs it off)
//   BENCH_DUMP=file     per-frame latency, "<frame> <ms>" per line
// Prints one JSON line of results. See docs/WINDOWS_DECODE_PIPELINE.md.
//
// Build (MSYS2 UCRT64):
//   gcc -O2 decode_bench.c -o decode_bench.exe \
//       $(pkg-config --cflags --libs libavformat libavcodec libavutil) -lvulkan-1 -lwinmm
#define VK_USE_PLATFORM_WIN32_KHR
#include <windows.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <vulkan/vulkan.h>
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_vulkan.h>
#include <libavutil/opt.h>

static double now_ms(void) {
    static LARGE_INTEGER f; LARGE_INTEGER c;
    if (!f.QuadPart) QueryPerformanceFrequency(&f);
    QueryPerformanceCounter(&c);
    return (double)c.QuadPart * 1000.0 / (double)f.QuadPart;
}
static void sleep_until(double t) {
    for (;;) {
        double d = t - now_ms();
        if (d <= 0) return;
        if (d > 2.0) Sleep((DWORD)(d - 1.5)); else SwitchToThread();
    }
}

#define MAXF 4096
static AVPacket *g_pkts[MAXF];
static int g_npkts;
static double g_sched[MAXF], g_done[MAXF], g_net_block[MAXF], g_net_late[MAXF];
static int g_frames_out;
static AVCodecContext *g_ctx;
static VkDevice g_dev;
static int g_mode; // 0 direct 1 thread 2 pull
static double g_fps = 120;
static volatile LONG g_net_done;
static int g_overflows;

// --- bounded queue (moonlight-common-c's decodeUnitQueue is 15 deep) ---
#define QCAP 15
static int q[QCAP], qh, qn;
static CRITICAL_SECTION qcs; static CONDITION_VARIABLE qcv;
static int q_push(int i) {
    EnterCriticalSection(&qcs);
    if (qn == QCAP) { qn = 0; g_overflows++; LeaveCriticalSection(&qcs); return 0; } // flush, like LbqFlushQueueItems
    q[(qh + qn++) % QCAP] = i;
    WakeConditionVariable(&qcv);
    LeaveCriticalSection(&qcs);
    return 1;
}
static int q_pop(int wait) {
    EnterCriticalSection(&qcs);
    while (wait && qn == 0 && !g_net_done) SleepConditionVariableCS(&qcv, &qcs, 50);
    int r = -1;
    if (qn) { r = q[qh]; qh = (qh + 1) % QCAP; qn--; }
    LeaveCriticalSection(&qcs);
    return r;
}

// --- "renderer": waits on each frame's timeline semaphore, like the fixed
// vk_render_frame_vkimage does, and stamps completion ---
typedef struct { AVFrame *f; VkSemaphore sem; uint64_t val; int idx; } RItem;
#define RCAP 64
static RItem rq[RCAP]; static int rh, rn;
static CRITICAL_SECTION rcs; static CONDITION_VARIABLE rcv;
static volatile LONG g_dec_done;
static DWORD WINAPI render_thread(LPVOID a) {
    (void)a;
    for (;;) {
        EnterCriticalSection(&rcs);
        while (rn == 0 && !g_dec_done) SleepConditionVariableCS(&rcv, &rcs, 50);
        if (rn == 0 && g_dec_done) { LeaveCriticalSection(&rcs); break; }
        RItem it = rq[rh]; rh = (rh + 1) % RCAP; rn--;
        LeaveCriticalSection(&rcs);
        VkSemaphoreWaitInfo wi = { VK_STRUCTURE_TYPE_SEMAPHORE_WAIT_INFO };
        wi.semaphoreCount = 1; wi.pSemaphores = &it.sem; wi.pValues = &it.val;
        vkWaitSemaphores(g_dev, &wi, 2000000000ULL);
        if (it.idx >= 0 && it.idx < MAXF) g_done[it.idx] = now_ms();
        av_frame_free(&it.f);
    }
    return 0;
}

static void deliver(AVFrame *fr) {
    AVVkFrame *vkf = (AVVkFrame *)fr->data[0];
    RItem it = { av_frame_clone(fr), vkf->sem[0], vkf->sem_value[0], (int)fr->pts };
    EnterCriticalSection(&rcs);
    if (rn < RCAP) { rq[(rh + rn++) % RCAP] = it; WakeConditionVariable(&rcv); }
    else av_frame_free(&it.f);
    LeaveCriticalSection(&rcs);
    g_frames_out++;
}

static double g_send_ms[MAXF];
static int dec_send(int i) {
    AVPacket *p = av_packet_clone(g_pkts[i]);
    p->pts = i;
    double t = now_ms();
    int r = avcodec_send_packet(g_ctx, p);
    if (i >= 0 && i < MAXF) g_send_ms[i] = now_ms() - t;
    av_packet_free(&p);
    return r;
}
static int dec_drain(void) {
    AVFrame *f = av_frame_alloc(); int n = 0;
    while (avcodec_receive_frame(g_ctx, f) == 0) { deliver(f); av_frame_unref(f); n++; }
    av_frame_free(&f);
    return n;
}

static DWORD WINAPI net_thread(LPVOID a) {
    (void)a;
    double t0 = now_ms() + 50, iv = 1000.0 / g_fps;
    for (int i = 0; i < g_npkts; i++) {
        g_sched[i] = t0 + i * iv;
        sleep_until(g_sched[i]);
        double s = now_ms();
        g_net_late[i] = s - g_sched[i]; // >0: still busy with the previous unit (socket backlog)
        if (g_mode == 0) { if (dec_send(i) == 0) dec_drain(); }
        else q_push(i);
        g_net_block[i] = now_ms() - s;
    }
    InterlockedExchange(&g_net_done, 1);
    WakeAllConditionVariable(&qcv);
    return 0;
}

static DWORD WINAPI dec_thread(LPVOID a) {
    (void)a;
    if (g_mode == 1) {
        for (;;) { int i = q_pop(1); if (i < 0) { if (g_net_done) break; continue; } if (dec_send(i) == 0) dec_drain(); }
    } else { // pull, moonlight-qt decoderThreadProc
        unsigned in = 0, out = 0;
        for (;;) {
            if (in == out) {
                int i = q_pop(1);
                if (i < 0) { if (g_net_done) break; continue; }
                if (dec_send(i) == 0) in++;
            }
            while (in != out) {
                int got = dec_drain();
                if (got > 0) { out += got; if (out > in) out = in; break; }
                int i = q_pop(0);
                if (i >= 0) { if (dec_send(i) == 0) in++; }
                else if (g_net_done) { out = in; break; }
                else Sleep(1);
            }
        }
    }
    return 0;
}

static enum AVPixelFormat get_fmt(AVCodecContext *c, const enum AVPixelFormat *f) {
    (void)c; for (; *f != AV_PIX_FMT_NONE; f++) if (*f == AV_PIX_FMT_VULKAN) return *f; return AV_PIX_FMT_NONE;
}
static int cmpd(const void *a, const void *b) { double x = *(double*)a, y = *(double*)b; return x < y ? -1 : x > y; }
static double pct(double *v, int n, double p) { if (!n) return 0; int k = (int)(p * (n - 1)); return v[k]; }
static double ft2ms(FILETIME f) { ULARGE_INTEGER u; u.LowPart = f.dwLowDateTime; u.HighPart = f.dwHighDateTime; return u.QuadPart / 10000.0; }

int main(int argc, char **argv) {
    if (argc < 3) { fprintf(stderr, "usage: %s clip direct|thread|pull [fps] [seconds]\n", argv[0]); return 2; }
    g_mode = !strcmp(argv[2], "direct") ? 0 : !strcmp(argv[2], "thread") ? 1 : 2;
    if (argc > 3) g_fps = atof(argv[3]);
    double secs = argc > 4 ? atof(argv[4]) : 10;
    timeBeginPeriod(1);
    av_log_set_level(AV_LOG_ERROR);
    InitializeCriticalSection(&qcs); InitializeConditionVariable(&qcv);
    InitializeCriticalSection(&rcs); InitializeConditionVariable(&rcv);

    AVFormatContext *fmt = NULL;
    if (avformat_open_input(&fmt, argv[1], NULL, NULL) < 0) return 1;
    avformat_find_stream_info(fmt, NULL);
    AVPacket *p = av_packet_alloc();
    int maxp = (int)(secs * g_fps); if (maxp > MAXF) maxp = MAXF;
    while (g_npkts < maxp && av_read_frame(fmt, p) >= 0) { g_pkts[g_npkts++] = av_packet_clone(p); av_packet_unref(p); }

    AVDictionary *o = NULL;
    av_dict_set(&o, "instance_extensions", "+VK_KHR_surface+VK_KHR_win32_surface", 0);
    av_dict_set(&o, "device_extensions", "+VK_KHR_swapchain", 0);
    AVBufferRef *hw = NULL;
    if (av_hwdevice_ctx_create(&hw, AV_HWDEVICE_TYPE_VULKAN, NULL, o, 0) < 0) { fprintf(stderr, "no vulkan\n"); return 1; }
    AVVulkanDeviceContext *vk = (AVVulkanDeviceContext *)((AVHWDeviceContext *)hw->data)->hwctx;
    g_dev = vk->act_dev;
    VkPhysicalDeviceProperties pr; vkGetPhysicalDeviceProperties(vk->phys_dev, &pr);

    const AVCodec *codec = avcodec_find_decoder(AV_CODEC_ID_HEVC);
    g_ctx = avcodec_alloc_context3(codec);
    if (!getenv("BENCH_NOLOWDELAY")) g_ctx->flags |= AV_CODEC_FLAG_LOW_DELAY;
    g_ctx->hw_device_ctx = av_buffer_ref(hw);
    g_ctx->get_format = get_fmt;
    if (getenv("BENCH_THREADS")) { g_ctx->thread_count = atoi(getenv("BENCH_THREADS")); g_ctx->thread_type = FF_THREAD_FRAME; }
    if (avcodec_open2(g_ctx, codec, NULL) < 0) return 1;

    // warm-up: first IDR + hw frames pool creation, outside the measurement
    // (index -1 so the renderer doesn't stamp it)
    { AVPacket *w = av_packet_clone(g_pkts[0]); w->pts = -1; avcodec_send_packet(g_ctx, w); av_packet_free(&w); }
    HANDLE rt = CreateThread(NULL, 0, render_thread, NULL, 0, NULL);
    dec_drain(); g_frames_out = 0; Sleep(200);

    FILETIME c0, e0, k0, u0, k1, u1;
    GetProcessTimes(GetCurrentProcess(), &c0, &e0, &k0, &u0);
    double w0 = now_ms();
    HANDLE dt = g_mode ? CreateThread(NULL, 0, dec_thread, NULL, 0, NULL) : NULL;
    HANDLE nt = CreateThread(NULL, 0, net_thread, NULL, 0, NULL);
    WaitForSingleObject(nt, INFINITE);
    if (dt) WaitForSingleObject(dt, INFINITE);
    avcodec_send_packet(g_ctx, NULL); dec_drain();
    InterlockedExchange(&g_dec_done, 1); WakeAllConditionVariable(&rcv);
    WaitForSingleObject(rt, INFINITE);
    double wall = now_ms() - w0;
    GetProcessTimes(GetCurrentProcess(), &c0, &e0, &k1, &u1);
    double cpu = (ft2ms(k1) - ft2ms(k0)) + (ft2ms(u1) - ft2ms(u0));

    if (getenv("BENCH_DUMP")) { FILE *df = fopen(getenv("BENCH_DUMP"), "w"); for (int i = 1; i < g_npkts; i++) fprintf(df, "%d %.2f\n", i, g_done[i] > 0 ? g_done[i] - g_sched[i] : -1); fclose(df); }
    // frame 0 is the warm-up IDR resent; measure from frame 1
    static double lat[MAXF], blk[MAXF], late[MAXF]; int n = 0, nb = 0;
    for (int i = 1; i < g_npkts; i++) {
        blk[nb] = g_net_block[i]; late[nb++] = g_net_late[i];
        if (g_done[i] > 0) lat[n++] = g_done[i] - g_sched[i];
    }
    static double snd[MAXF]; for (int i = 1; i < g_npkts; i++) snd[i-1] = g_send_ms[i];
    qsort(snd, g_npkts-1, sizeof(double), cmpd);
    fprintf(stderr, "send_packet p50=%.2fms p99=%.2fms\n", pct(snd, g_npkts-1, .5), pct(snd, g_npkts-1, .99));
    qsort(lat, n, sizeof(double), cmpd); qsort(blk, nb, sizeof(double), cmpd); qsort(late, nb, sizeof(double), cmpd);
    double span = g_sched[g_npkts - 1] - g_sched[1];
    printf("{\"gpu\":\"%s\",\"clip\":\"%s\",\"mode\":\"%s\",\"fps_in\":%.0f,\"frames_in\":%d,\"frames_decoded\":%d,"
           "\"fps_out\":%.1f,\"lat_p50\":%.2f,\"lat_p95\":%.2f,\"lat_p99\":%.2f,\"lat_max\":%.2f,"
           "\"net_block_p50\":%.2f,\"net_block_p99\":%.2f,\"net_block_max\":%.2f,\"net_late_p99\":%.2f,\"net_late_max\":%.2f,"
           "\"overflows\":%d,\"cpu_pct_of_one_core\":%.1f}\n",
           pr.deviceName, argv[1], argv[2], g_fps, nb, n, n / (span / 1000.0),
           pct(lat, n, .5), pct(lat, n, .95), pct(lat, n, .99), n ? lat[n - 1] : 0,
           pct(blk, nb, .5), pct(blk, nb, .99), blk[nb - 1], pct(late, nb, .99), late[nb - 1],
           g_overflows, 100.0 * cpu / wall);
    return 0;
}
