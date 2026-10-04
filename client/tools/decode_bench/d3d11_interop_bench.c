// d3d11_interop_bench: D3D11VA decode -> GPU copy into a shared NV12 texture
// -> imported into Vulkan, synchronized with a shared ID3D11Fence imported as
// a Vulkan timeline semaphore. This is the path the Windows client uses to
// get moonlight-qt's decoder (D3D11VA) into our Vulkan renderer.
//
// Same pacing/latency method as decode_bench.c: a "network" thread releases
// one access unit every 1/fps s into a 15-deep queue; a decode thread
// (moonlight-qt loop) decodes and copies; a "render" thread waits on the
// Vulkan side of the fence and stamps the frame. Latency = scheduled arrival
// -> frame readable by Vulkan.
//
// On the first measured frame it also proves the interop is real: plane 0
// read back through Vulkan must byte-match av_hwframe_transfer_data's copy.
//
// usage: d3d11_interop_bench <clip.hevc> [fps] [seconds]
// Build (MSYS2 UCRT64):
//   gcc -O2 d3d11_interop_bench.c -o d3d11_interop_bench.exe \
//       $(pkg-config --cflags --libs libavformat libavcodec libavutil) \
//       -lvulkan-1 -ld3d11 -ldxgi -lwinmm
#define COBJMACROS
#define INITGUID
#define VK_USE_PLATFORM_WIN32_KHR
#include <windows.h>
#include <d3d11_4.h>
#include <dxgi1_2.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <vulkan/vulkan.h>
#include <vulkan/vulkan_win32.h>
#include <libavformat/avformat.h>
#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_d3d11va.h>
#include <libavutil/hwcontext_vulkan.h>

static double now_ms(void) {
    static LARGE_INTEGER f; LARGE_INTEGER c;
    if (!f.QuadPart) QueryPerformanceFrequency(&f);
    QueryPerformanceCounter(&c);
    return (double)c.QuadPart * 1000.0 / (double)f.QuadPart;
}
static void sleep_until(double t) {
    for (;;) { double d = t - now_ms(); if (d <= 0) return; if (d > 2.0) Sleep((DWORD)(d - 1.5)); else SwitchToThread(); }
}
#define CHECK(x, msg) do { if (!(x)) { fprintf(stderr, "FAIL: %s (line %d)\n", msg, __LINE__); exit(1); } } while (0)

#define MAXF 4096
#define RING 3
static AVPacket *g_pkts[MAXF];
static int g_npkts;
static double g_sched[MAXF], g_done[MAXF], g_send_ms[MAXF];
static AVCodecContext *g_ctx;
static double g_fps = 120;
static volatile LONG g_net_done, g_dec_done;
static int g_overflows;

// D3D11 side
static ID3D11Device5 *g_d3d;
static ID3D11DeviceContext4 *g_d3dctx;
static AVD3D11VADeviceContext *g_avd3d;
static ID3D11Texture2D *g_shared[RING];
static ID3D11Fence *g_fence;
static UINT64 g_fence_val;
// Vulkan side
static VkDevice g_dev;
static VkPhysicalDevice g_phys;
static VkImage g_vkimg[RING];
static VkDeviceMemory g_vkmem[RING];
static VkSemaphore g_vksem;
static int g_w, g_h;

// --- queue (moonlight-common-c's 15-deep decode unit queue) ---
#define QCAP 15
static int q[QCAP], qh, qn;
static CRITICAL_SECTION qcs; static CONDITION_VARIABLE qcv;
static void q_push(int i) {
    EnterCriticalSection(&qcs);
    if (qn == QCAP) { qn = 0; g_overflows++; }
    q[(qh + qn++) % QCAP] = i;
    WakeConditionVariable(&qcv);
    LeaveCriticalSection(&qcs);
}
static int q_pop(int wait) {
    EnterCriticalSection(&qcs);
    while (wait && qn == 0 && !g_net_done) SleepConditionVariableCS(&qcv, &qcs, 50);
    int r = -1;
    if (qn) { r = q[qh]; qh = (qh + 1) % QCAP; qn--; }
    LeaveCriticalSection(&qcs);
    return r;
}

// --- render side: waits on the imported fence value for each frame ---
typedef struct { UINT64 val; int idx; } RItem;
#define RCAP 64
static RItem rq[RCAP]; static int rh, rn;
static CRITICAL_SECTION rcs; static CONDITION_VARIABLE rcv;
static DWORD WINAPI render_thread(LPVOID a) {
    (void)a;
    for (;;) {
        EnterCriticalSection(&rcs);
        while (rn == 0 && !g_dec_done) SleepConditionVariableCS(&rcv, &rcs, 50);
        if (rn == 0 && g_dec_done) { LeaveCriticalSection(&rcs); break; }
        RItem it = rq[rh]; rh = (rh + 1) % RCAP; rn--;
        LeaveCriticalSection(&rcs);
        VkSemaphoreWaitInfo wi = { VK_STRUCTURE_TYPE_SEMAPHORE_WAIT_INFO };
        wi.semaphoreCount = 1; wi.pSemaphores = &g_vksem; wi.pValues = &it.val;
        VkResult r = vkWaitSemaphores(g_dev, &wi, 2000000000ULL);
        if (r == VK_SUCCESS && it.idx >= 0 && it.idx < MAXF) g_done[it.idx] = now_ms();
    }
    return 0;
}

static int g_ring_next;
static void deliver(AVFrame *fr) {
    ID3D11Texture2D *src = (ID3D11Texture2D *)fr->data[0];
    UINT slice = (UINT)(intptr_t)fr->data[1];
    int slot = g_ring_next; g_ring_next = (g_ring_next + 1) % RING;
    g_avd3d->lock(g_avd3d->lock_ctx);
    D3D11_BOX box = { 0, 0, 0, (UINT)g_w, (UINT)g_h, 1 };
    ID3D11DeviceContext4_CopySubresourceRegion(g_d3dctx, (ID3D11Resource *)g_shared[slot], 0, 0, 0, 0,
                                               (ID3D11Resource *)src, slice, &box);
    UINT64 v = ++g_fence_val;
    ID3D11DeviceContext4_Signal(g_d3dctx, g_fence, v);
    ID3D11DeviceContext4_Flush(g_d3dctx);
    g_avd3d->unlock(g_avd3d->lock_ctx);
    RItem it = { v, (int)fr->pts };
    EnterCriticalSection(&rcs);
    if (rn < RCAP) { rq[(rh + rn++) % RCAP] = it; WakeConditionVariable(&rcv); }
    LeaveCriticalSection(&rcs);
}

static int dec_send(int i) {
    AVPacket *p = av_packet_clone(g_pkts[i]);
    p->pts = i;
    double t = now_ms();
    int r = avcodec_send_packet(g_ctx, p);
    if (i >= 0) g_send_ms[i] = now_ms() - t;
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
    for (int i = 1; i < g_npkts; i++) { g_sched[i] = t0 + i * iv; sleep_until(g_sched[i]); q_push(i); }
    InterlockedExchange(&g_net_done, 1); WakeAllConditionVariable(&qcv);
    return 0;
}
static DWORD WINAPI dec_thread(LPVOID a) {
    (void)a;
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
    return 0;
}

static enum AVPixelFormat get_fmt(AVCodecContext *c, const enum AVPixelFormat *f) {
    (void)c; for (; *f != AV_PIX_FMT_NONE; f++) if (*f == AV_PIX_FMT_D3D11) return *f; return AV_PIX_FMT_NONE;
}
static uint32_t find_mem_type(uint32_t bits, VkMemoryPropertyFlags want) {
    VkPhysicalDeviceMemoryProperties mp; vkGetPhysicalDeviceMemoryProperties(g_phys, &mp);
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++)
        if ((bits & (1u << i)) && (mp.memoryTypes[i].propertyFlags & want) == want) return i;
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++) if (bits & (1u << i)) return i;
    return UINT32_MAX;
}

// Plane-0 readback through Vulkan, compared with ffmpeg's own CPU copy.
static void verify_interop(AVFrame *fr, int slot, uint32_t qf, VkQueue queue) {
    AVFrame *sw = av_frame_alloc();
    CHECK(av_hwframe_transfer_data(sw, fr, 0) == 0, "av_hwframe_transfer_data");
    size_t ysize = (size_t)g_w * g_h;
    VkBufferCreateInfo bci = { VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO };
    bci.size = ysize; bci.usage = VK_BUFFER_USAGE_TRANSFER_DST_BIT;
    VkBuffer buf; CHECK(vkCreateBuffer(g_dev, &bci, NULL, &buf) == VK_SUCCESS, "vkCreateBuffer");
    VkMemoryRequirements mr; vkGetBufferMemoryRequirements(g_dev, buf, &mr);
    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
    mai.allocationSize = mr.size;
    mai.memoryTypeIndex = find_mem_type(mr.memoryTypeBits, VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
    VkDeviceMemory bm; CHECK(vkAllocateMemory(g_dev, &mai, NULL, &bm) == VK_SUCCESS, "alloc readback");
    vkBindBufferMemory(g_dev, buf, bm, 0);

    VkCommandPoolCreateInfo pci = { VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO }; pci.queueFamilyIndex = qf;
    VkCommandPool pool; vkCreateCommandPool(g_dev, &pci, NULL, &pool);
    VkCommandBufferAllocateInfo cai = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO };
    cai.commandPool = pool; cai.level = VK_COMMAND_BUFFER_LEVEL_PRIMARY; cai.commandBufferCount = 1;
    VkCommandBuffer cb; vkAllocateCommandBuffers(g_dev, &cai, &cb);
    VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO };
    vkBeginCommandBuffer(cb, &bi);
    VkImageMemoryBarrier b = { VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER };
    b.oldLayout = VK_IMAGE_LAYOUT_GENERAL; // written by D3D11: acquire from EXTERNAL, keeping contents
    b.newLayout = VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL;
    b.srcQueueFamilyIndex = VK_QUEUE_FAMILY_EXTERNAL; b.dstQueueFamilyIndex = qf;
    b.image = g_vkimg[slot];
    b.subresourceRange.aspectMask = VK_IMAGE_ASPECT_PLANE_0_BIT; b.subresourceRange.levelCount = 1; b.subresourceRange.layerCount = 1;
    b.dstAccessMask = VK_ACCESS_TRANSFER_READ_BIT;
    vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT, 0, 0, NULL, 0, NULL, 1, &b);
    VkBufferImageCopy reg = { 0 };
    reg.imageSubresource.aspectMask = VK_IMAGE_ASPECT_PLANE_0_BIT; reg.imageSubresource.layerCount = 1;
    reg.imageExtent.width = g_w; reg.imageExtent.height = g_h; reg.imageExtent.depth = 1;
    vkCmdCopyImageToBuffer(cb, g_vkimg[slot], VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, buf, 1, &reg);
    vkEndCommandBuffer(cb);
    UINT64 wv = g_fence_val;
    VkTimelineSemaphoreSubmitInfo ts = { VK_STRUCTURE_TYPE_TIMELINE_SEMAPHORE_SUBMIT_INFO };
    ts.waitSemaphoreValueCount = 1; ts.pWaitSemaphoreValues = &wv;
    VkPipelineStageFlags ws = VK_PIPELINE_STAGE_TRANSFER_BIT;
    VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO };
    si.pNext = &ts; si.waitSemaphoreCount = 1; si.pWaitSemaphores = &g_vksem; si.pWaitDstStageMask = &ws;
    si.commandBufferCount = 1; si.pCommandBuffers = &cb;
    CHECK(vkQueueSubmit(queue, 1, &si, VK_NULL_HANDLE) == VK_SUCCESS, "vkQueueSubmit verify");
    vkQueueWaitIdle(queue);
    uint8_t *p; vkMapMemory(g_dev, bm, 0, ysize, 0, (void **)&p);
    size_t diff = 0; uint64_t sum = 0;
    for (int y = 0; y < g_h; y++) {
        const uint8_t *a = p + (size_t)y * g_w, *c = sw->data[0] + (size_t)y * sw->linesize[0];
        for (int x = 0; x < g_w; x++) { diff += a[x] != c[x]; sum += a[x]; }
    }
    printf("interop check: luma bytes differing=%zu of %zu, mean luma via Vulkan=%.1f -> %s\n",
           diff, ysize, (double)sum / ysize, diff == 0 && sum > 0 ? "OK" : "MISMATCH");
    vkUnmapMemory(g_dev, bm);
    vkDestroyCommandPool(g_dev, pool, NULL); vkDestroyBuffer(g_dev, buf, NULL); vkFreeMemory(g_dev, bm, NULL);
    av_frame_free(&sw);
}

static int cmpd(const void *a, const void *b) { double x = *(double *)a, y = *(double *)b; return x < y ? -1 : x > y; }
static double pct(double *v, int n, double p) { return n ? v[(int)(p * (n - 1))] : 0; }

int main(int argc, char **argv) {
    if (argc < 2) { fprintf(stderr, "usage: %s clip [fps] [seconds]\n", argv[0]); return 2; }
    if (argc > 2) g_fps = atof(argv[2]);
    double secs = argc > 3 ? atof(argv[3]) : 10;
    timeBeginPeriod(1);
    av_log_set_level(AV_LOG_ERROR);
    InitializeCriticalSection(&qcs); InitializeConditionVariable(&qcv);
    InitializeCriticalSection(&rcs); InitializeConditionVariable(&rcv);

    AVFormatContext *fmt = NULL;
    CHECK(avformat_open_input(&fmt, argv[1], NULL, NULL) == 0, "open clip");
    avformat_find_stream_info(fmt, NULL);
    g_w = fmt->streams[0]->codecpar->width; g_h = fmt->streams[0]->codecpar->height;
    AVPacket *pk = av_packet_alloc();
    int maxp = (int)(secs * g_fps); if (maxp > MAXF) maxp = MAXF;
    while (g_npkts < maxp && av_read_frame(fmt, pk) >= 0) { g_pkts[g_npkts++] = av_packet_clone(pk); av_packet_unref(pk); }

    // Vulkan device exactly like the client (vk_hwdev_bridge_windows.c).
    AVDictionary *o = NULL;
    av_dict_set(&o, "instance_extensions", "+VK_KHR_surface+VK_KHR_win32_surface", 0);
    av_dict_set(&o, "device_extensions", "+VK_KHR_swapchain+VK_KHR_external_memory_win32+VK_KHR_external_semaphore_win32", 0);
    AVBufferRef *vkhw = NULL;
    CHECK(av_hwdevice_ctx_create(&vkhw, AV_HWDEVICE_TYPE_VULKAN, NULL, o, 0) == 0, "vulkan hwdevice");
    AVVulkanDeviceContext *vk = (AVVulkanDeviceContext *)((AVHWDeviceContext *)vkhw->data)->hwctx;
    g_dev = vk->act_dev; g_phys = vk->phys_dev;
    VkPhysicalDeviceIDProperties idp = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_ID_PROPERTIES };
    VkPhysicalDeviceProperties2 pp = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2 }; pp.pNext = &idp;
    vkGetPhysicalDeviceProperties2(g_phys, &pp);
    CHECK(idp.deviceLUIDValid, "Vulkan device LUID");
    uint32_t gfx_qf = UINT32_MAX;
    for (int i = 0; i < vk->nb_qf; i++) if (vk->qf[i].flags & VK_QUEUE_GRAPHICS_BIT) { gfx_qf = vk->qf[i].idx; break; }
    VkQueue gfx_q; vkGetDeviceQueue(g_dev, gfx_qf, 0, &gfx_q);

    // D3D11 device on the same adapter (LUID match), wrapped for ffmpeg.
    IDXGIFactory1 *fac; CHECK(CreateDXGIFactory1(&IID_IDXGIFactory1, (void **)&fac) == S_OK, "DXGI factory");
    IDXGIAdapter1 *ad = NULL, *pick = NULL;
    for (UINT i = 0; IDXGIFactory1_EnumAdapters1(fac, i, &ad) == S_OK; i++) {
        DXGI_ADAPTER_DESC1 d; IDXGIAdapter1_GetDesc1(ad, &d);
        if (!memcmp(&d.AdapterLuid, idp.deviceLUID, sizeof(LUID))) { pick = ad; break; }
        IDXGIAdapter1_Release(ad);
    }
    CHECK(pick, "no DXGI adapter with the Vulkan device's LUID");
    ID3D11Device *dev0; ID3D11DeviceContext *ctx0;
    CHECK(D3D11CreateDevice((IDXGIAdapter *)pick, D3D_DRIVER_TYPE_UNKNOWN, NULL, D3D11_CREATE_DEVICE_VIDEO_SUPPORT,
                            NULL, 0, D3D11_SDK_VERSION, &dev0, NULL, &ctx0) == S_OK, "D3D11CreateDevice");
    CHECK(ID3D11Device_QueryInterface(dev0, &IID_ID3D11Device5, (void **)&g_d3d) == S_OK, "ID3D11Device5 (fences)");
    CHECK(ID3D11DeviceContext_QueryInterface(ctx0, &IID_ID3D11DeviceContext4, (void **)&g_d3dctx) == S_OK, "ID3D11DeviceContext4");
    AVBufferRef *dhw = av_hwdevice_ctx_alloc(AV_HWDEVICE_TYPE_D3D11VA);
    g_avd3d = (AVD3D11VADeviceContext *)((AVHWDeviceContext *)dhw->data)->hwctx;
    g_avd3d->device = dev0; // ffmpeg takes this reference
    CHECK(av_hwdevice_ctx_init(dhw) == 0, "d3d11va hwdevice init");

    // Shared NV12 ring + shared fence, imported into Vulkan.
    PFN_vkGetMemoryWin32HandlePropertiesKHR getProps = (PFN_vkGetMemoryWin32HandlePropertiesKHR)vkGetDeviceProcAddr(g_dev, "vkGetMemoryWin32HandlePropertiesKHR");
    PFN_vkImportSemaphoreWin32HandleKHR importSem = (PFN_vkImportSemaphoreWin32HandleKHR)vkGetDeviceProcAddr(g_dev, "vkImportSemaphoreWin32HandleKHR");
    CHECK(getProps && importSem, "external memory/semaphore win32 entry points");
    for (int s = 0; s < RING; s++) {
        D3D11_TEXTURE2D_DESC td = { 0 };
        td.Width = g_w; td.Height = g_h; td.MipLevels = 1; td.ArraySize = 1; td.Format = DXGI_FORMAT_NV12;
        td.SampleDesc.Count = 1; td.Usage = D3D11_USAGE_DEFAULT; td.BindFlags = D3D11_BIND_SHADER_RESOURCE;
        td.MiscFlags = D3D11_RESOURCE_MISC_SHARED | D3D11_RESOURCE_MISC_SHARED_NTHANDLE;
        CHECK(ID3D11Device5_CreateTexture2D(g_d3d, &td, NULL, &g_shared[s]) == S_OK, "shared NV12 texture");
        IDXGIResource1 *r; ID3D11Texture2D_QueryInterface(g_shared[s], &IID_IDXGIResource1, (void **)&r);
        HANDLE h; CHECK(IDXGIResource1_CreateSharedHandle(r, NULL, DXGI_SHARED_RESOURCE_READ | DXGI_SHARED_RESOURCE_WRITE, NULL, &h) == S_OK, "CreateSharedHandle");
        IDXGIResource1_Release(r);

        VkExternalMemoryImageCreateInfo emi = { VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO };
        emi.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_D3D11_TEXTURE_BIT;
        VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
        ici.pNext = &emi; ici.imageType = VK_IMAGE_TYPE_2D; ici.format = VK_FORMAT_G8_B8R8_2PLANE_420_UNORM;
        ici.extent.width = g_w; ici.extent.height = g_h; ici.extent.depth = 1; ici.mipLevels = 1; ici.arrayLayers = 1;
        ici.samples = VK_SAMPLE_COUNT_1_BIT; ici.tiling = VK_IMAGE_TILING_OPTIMAL;
        ici.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
        ici.flags = 0;
        ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        CHECK(vkCreateImage(g_dev, &ici, NULL, &g_vkimg[s]) == VK_SUCCESS, "vkCreateImage (external NV12)");
        VkMemoryWin32HandlePropertiesKHR hp = { VK_STRUCTURE_TYPE_MEMORY_WIN32_HANDLE_PROPERTIES_KHR };
        CHECK(getProps(g_dev, VK_EXTERNAL_MEMORY_HANDLE_TYPE_D3D11_TEXTURE_BIT, h, &hp) == VK_SUCCESS, "vkGetMemoryWin32HandlePropertiesKHR");
        VkMemoryRequirements mr; vkGetImageMemoryRequirements(g_dev, g_vkimg[s], &mr);
        VkMemoryDedicatedAllocateInfo ded = { VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO }; ded.image = g_vkimg[s];
        VkImportMemoryWin32HandleInfoKHR imp = { VK_STRUCTURE_TYPE_IMPORT_MEMORY_WIN32_HANDLE_INFO_KHR };
        imp.pNext = &ded; imp.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_D3D11_TEXTURE_BIT; imp.handle = h;
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.pNext = &imp; mai.allocationSize = mr.size;
        mai.memoryTypeIndex = find_mem_type(mr.memoryTypeBits & hp.memoryTypeBits, 0);
        CHECK(vkAllocateMemory(g_dev, &mai, NULL, &g_vkmem[s]) == VK_SUCCESS, "import D3D11 texture memory");
        CHECK(vkBindImageMemory(g_dev, g_vkimg[s], g_vkmem[s], 0) == VK_SUCCESS, "bind imported memory");
        CloseHandle(h);
    }
    CHECK(ID3D11Device5_CreateFence(g_d3d, 0, D3D11_FENCE_FLAG_SHARED, &IID_ID3D11Fence, (void **)&g_fence) == S_OK, "ID3D11Fence");
    HANDLE fh; CHECK(ID3D11Fence_CreateSharedHandle(g_fence, NULL, GENERIC_ALL, NULL, &fh) == S_OK, "fence shared handle");
    VkSemaphoreTypeCreateInfo stci = { VK_STRUCTURE_TYPE_SEMAPHORE_TYPE_CREATE_INFO }; stci.semaphoreType = VK_SEMAPHORE_TYPE_TIMELINE;
    VkSemaphoreCreateInfo sci = { VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO }; sci.pNext = &stci;
    CHECK(vkCreateSemaphore(g_dev, &sci, NULL, &g_vksem) == VK_SUCCESS, "timeline semaphore");
    VkImportSemaphoreWin32HandleInfoKHR isi = { VK_STRUCTURE_TYPE_IMPORT_SEMAPHORE_WIN32_HANDLE_INFO_KHR };
    isi.semaphore = g_vksem; isi.handleType = VK_EXTERNAL_SEMAPHORE_HANDLE_TYPE_D3D11_FENCE_BIT; isi.handle = fh;
    CHECK(importSem(g_dev, &isi) == VK_SUCCESS, "import ID3D11Fence as timeline semaphore");

    const AVCodec *codec = avcodec_find_decoder(AV_CODEC_ID_HEVC);
    g_ctx = avcodec_alloc_context3(codec);
    g_ctx->flags |= AV_CODEC_FLAG_LOW_DELAY;
    g_ctx->hw_device_ctx = av_buffer_ref(dhw);
    g_ctx->get_format = get_fmt;
    CHECK(avcodec_open2(g_ctx, codec, NULL) == 0, "avcodec_open2 d3d11va");

    VkPhysicalDeviceProperties pr; vkGetPhysicalDeviceProperties(g_phys, &pr);
    printf("GPU: %s, %dx%d, D3D11VA decode -> shared NV12 -> Vulkan\n", pr.deviceName, g_w, g_h);

    // Warm-up on the IDR, then prove the interop on that frame.
    dec_send(0);
    {
        AVFrame *f = av_frame_alloc();
        CHECK(avcodec_receive_frame(g_ctx, f) == 0, "first frame");
        f->pts = -1;
        int slot = g_ring_next;
        deliver(f);
        verify_interop(f, slot, gfx_qf, gfx_q);
        av_frame_free(&f);
    }
    HANDLE rt = CreateThread(NULL, 0, render_thread, NULL, 0, NULL);
    Sleep(200);

    FILETIME c0, e0, k0, u0, k1, u1;
    GetProcessTimes(GetCurrentProcess(), &c0, &e0, &k0, &u0);
    double w0 = now_ms();
    HANDLE dt = CreateThread(NULL, 0, dec_thread, NULL, 0, NULL);
    HANDLE nt = CreateThread(NULL, 0, net_thread, NULL, 0, NULL);
    WaitForSingleObject(nt, INFINITE); WaitForSingleObject(dt, INFINITE);
    avcodec_send_packet(g_ctx, NULL); dec_drain();
    InterlockedExchange(&g_dec_done, 1); WakeAllConditionVariable(&rcv);
    WaitForSingleObject(rt, INFINITE);
    double wall = now_ms() - w0;
    GetProcessTimes(GetCurrentProcess(), &c0, &e0, &k1, &u1);
    ULARGE_INTEGER a, b, c, d;
    a.LowPart = k0.dwLowDateTime; a.HighPart = k0.dwHighDateTime; b.LowPart = k1.dwLowDateTime; b.HighPart = k1.dwHighDateTime;
    c.LowPart = u0.dwLowDateTime; c.HighPart = u0.dwHighDateTime; d.LowPart = u1.dwLowDateTime; d.HighPart = u1.dwHighDateTime;
    double cpu = ((b.QuadPart - a.QuadPart) + (d.QuadPart - c.QuadPart)) / 10000.0;

    static double lat[MAXF], early[MAXF], snd[MAXF]; int n = 0, ne = 0, ns = 0;
    for (int i = 1; i < g_npkts; i++) {
        snd[ns++] = g_send_ms[i];
        if (g_done[i] <= 0) continue;
        double l = g_done[i] - g_sched[i];
        if (i <= 60) early[ne++] = l; else lat[n++] = l;
    }
    qsort(lat, n, sizeof(double), cmpd); qsort(early, ne, sizeof(double), cmpd); qsort(snd, ns, sizeof(double), cmpd);
    printf("{\"mode\":\"d3d11va+interop\",\"fps_in\":%.0f,\"frames_in\":%d,\"frames_done\":%d,"
           "\"lat_p50\":%.2f,\"lat_p99\":%.2f,\"lat_max\":%.2f,\"first05s_p50\":%.2f,\"send_p50\":%.2f,\"send_p99\":%.2f,"
           "\"overflows\":%d,\"cpu_pct_of_one_core\":%.1f}\n",
           g_fps, g_npkts - 1, n + ne, pct(lat, n, .5), pct(lat, n, .99), n ? lat[n - 1] : 0, pct(early, ne, .5),
           pct(snd, ns, .5), pct(snd, ns, .99), g_overflows, 100.0 * cpu / wall);
    return 0;
}
