// pyrowave_color_bench: the Windows client's PyroWave GPU decoder
// (internal/service/pyrowave_decode_windows.c, compiled in as-is) on every
// color mode a host can send: 4:2:0 / 4:4:4 x SDR (8-bit planes) / HDR
// (16-bit planes, BT.2020 PQ stamped in the sequence header the way rust-shine
// stamps it).
//
// Per mode: encode test frames with PyroWave's own encoder (fine 1-pixel
// chroma stripes, so a 4:4:4 stream that came out as 4:2:0 shows up), decode
// them through pyrowave_win_decode on an ffmpeg-created Vulkan device (the
// same kind the client renders with), read the ring image back, and compare
// every plane with PyroWave's CPU decode of the same access unit -- byte-exact
// for 8-bit, within one 8-bit step for 16-bit. Then time decode latency
// (submit -> timeline semaphore signalled) and check that the renderer can
// build a YCbCr sampler for the ring format.
//
// usage: pyrowave_color_bench <width> <height> <frames> <mbps>   (BENCH_VKDEV picks the GPU)
// Build (MSYS2 UCRT64), after scripts/build_pyrowave_windows.sh:
//   gcc -O2 pyrowave_color_bench.c ../../internal/service/pyrowave_decode_windows.c -o pyrowave_color_bench.exe \
//       -I../../third_party/pyrowave/vendor/pyrowave $(pkg-config --cflags --libs libavutil) \
//       -L../../third_party/pyrowave/build-win -lusbridge-pyrowave -lstdc++ -lvulkan-1
#define VK_USE_PLATFORM_WIN32_KHR
#include <windows.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <vulkan/vulkan.h>
#include <pyrowave.h>
#include <libavutil/dict.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_vulkan.h>

#define CHECK(x, msg) do { if (!(x)) { fprintf(stderr, "FAIL: %s (line %d)\n", msg, __LINE__); exit(1); } } while (0)

// The production decoder's hooks into the client (normally Go / the renderer).
static AVBufferRef *g_hw;
static CRITICAL_SECTION g_qlock;
void goVTLog(char *msg) { printf("  [client] %s\n", msg); }
AVBufferRef *win_vk_hwdev_ctx_ref(void) { return av_buffer_ref(g_hw); }
void vk_video_queue_lock(void) { EnterCriticalSection(&g_qlock); }
void vk_video_queue_unlock(void) { LeaveCriticalSection(&g_qlock); }
void vk_video_forget_image(void *img) { (void)img; }

int pyrowave_win_decode(const uint8_t *au, size_t len, void **out_img, int *out_vkfmt, void **out_sem,
                        uint64_t *out_val, int *out_w, int *out_h, void **out_slot);
void pyrowave_win_release_slot(void *ctx);
int pyrowave_win_color_supported(int c444, int hdr);

static VkDevice g_dev; static VkPhysicalDevice g_phys; static uint32_t g_qf; static VkQueue g_q;
static int W, H;

static double now_ms(void) {
    static LARGE_INTEGER f; LARGE_INTEGER c;
    if (!f.QuadPart) QueryPerformanceFrequency(&f);
    QueryPerformanceCounter(&c);
    return (double)c.QuadPart * 1000.0 / (double)f.QuadPart;
}

static uint32_t mem_type(uint32_t bits, VkMemoryPropertyFlags want) {
    VkPhysicalDeviceMemoryProperties mp; vkGetPhysicalDeviceMemoryProperties(g_phys, &mp);
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++)
        if ((bits & (1u << i)) && (mp.memoryTypes[i].propertyFlags & want) == want) return i;
    return 0;
}

static void wait_val(VkSemaphore sem, uint64_t v) {
    VkSemaphoreWaitInfo wi = { VK_STRUCTURE_TYPE_SEMAPHORE_WAIT_INFO };
    wi.semaphoreCount = 1; wi.pSemaphores = &sem; wi.pValues = &v;
    CHECK(vkWaitSemaphores(g_dev, &wi, 2000000000ULL) == VK_SUCCESS, "vkWaitSemaphores");
}

// Planes of a decoded ring image, tightly packed, bps bytes per sample.
static void readback(VkImage img, int c444, int bps, uint8_t *out) {
    int cw = c444 ? W : W / 2, ch = c444 ? H : H / 2;
    size_t ys = (size_t)W * H * bps, cs = (size_t)cw * ch * bps, total = ys + 2 * cs;
    VkBufferCreateInfo bci = { VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO }; bci.size = total; bci.usage = VK_BUFFER_USAGE_TRANSFER_DST_BIT;
    VkBuffer buf; vkCreateBuffer(g_dev, &bci, NULL, &buf);
    VkMemoryRequirements mr; vkGetBufferMemoryRequirements(g_dev, buf, &mr);
    VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO }; mai.allocationSize = mr.size;
    mai.memoryTypeIndex = mem_type(mr.memoryTypeBits, VK_MEMORY_PROPERTY_HOST_VISIBLE_BIT | VK_MEMORY_PROPERTY_HOST_COHERENT_BIT);
    VkDeviceMemory bm; vkAllocateMemory(g_dev, &mai, NULL, &bm); vkBindBufferMemory(g_dev, buf, bm, 0);
    VkCommandPoolCreateInfo pci = { VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO }; pci.queueFamilyIndex = g_qf;
    VkCommandPool pool; vkCreateCommandPool(g_dev, &pci, NULL, &pool);
    VkCommandBufferAllocateInfo cai = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO }; cai.commandPool = pool; cai.commandBufferCount = 1;
    VkCommandBuffer cb; vkAllocateCommandBuffers(g_dev, &cai, &cb);
    VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO }; vkBeginCommandBuffer(cb, &bi);
    VkImageMemoryBarrier b = { VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER };
    b.oldLayout = VK_IMAGE_LAYOUT_GENERAL; b.newLayout = VK_IMAGE_LAYOUT_GENERAL;
    b.srcQueueFamilyIndex = b.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED; b.image = img;
    b.subresourceRange.aspectMask = VK_IMAGE_ASPECT_PLANE_0_BIT | VK_IMAGE_ASPECT_PLANE_1_BIT | VK_IMAGE_ASPECT_PLANE_2_BIT;
    b.subresourceRange.levelCount = 1; b.subresourceRange.layerCount = 1;
    b.srcAccessMask = VK_ACCESS_SHADER_WRITE_BIT; b.dstAccessMask = VK_ACCESS_TRANSFER_READ_BIT;
    vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_ALL_COMMANDS_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT, 0, 0, NULL, 0, NULL, 1, &b);
    VkBufferImageCopy r[3] = { 0 };
    for (int p = 0; p < 3; p++) {
        r[p].bufferOffset = p == 0 ? 0 : ys + (p - 1) * cs;
        r[p].imageSubresource.aspectMask = VK_IMAGE_ASPECT_PLANE_0_BIT << p; r[p].imageSubresource.layerCount = 1;
        r[p].imageExtent.width = p ? cw : W; r[p].imageExtent.height = p ? ch : H; r[p].imageExtent.depth = 1;
    }
    vkCmdCopyImageToBuffer(cb, img, VK_IMAGE_LAYOUT_GENERAL, buf, 3, r);
    vkEndCommandBuffer(cb);
    VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO }; si.commandBufferCount = 1; si.pCommandBuffers = &cb;
    vk_video_queue_lock();
    CHECK(vkQueueSubmit(g_q, 1, &si, VK_NULL_HANDLE) == VK_SUCCESS, "submit readback");
    vkQueueWaitIdle(g_q);
    vk_video_queue_unlock();
    void *p; vkMapMemory(g_dev, bm, 0, total, 0, &p); memcpy(out, p, total); vkUnmapMemory(g_dev, bm);
    vkDestroyCommandPool(g_dev, pool, NULL); vkDestroyBuffer(g_dev, buf, NULL); vkFreeMemory(g_dev, bm, NULL);
}

// Moving gradients, a box, and 1-pixel red/blue chroma stripes in the lower half.
static void make_frame(int i, int c444, uint8_t *y, uint8_t *cb, uint8_t *cr) {
    int cw = c444 ? W : W / 2, ch = c444 ? H : H / 2, sx = c444 ? 1 : 2;
    for (int r = 0; r < H; r++)
        for (int c = 0; c < W; c++) {
            int v = 16 + ((c + i * 7) * 219 / W) % 220;
            if (c > W / 3 + i * 4 % W && c < W / 3 + i * 4 % W + W / 8 && r > H / 6 && r < H / 3) v = 235;
            y[(size_t)r * W + c] = (uint8_t)v;
        }
    for (int r = 0; r < ch; r++)
        for (int c = 0; c < cw; c++) {
            int x = c * sx;
            uint8_t u = (uint8_t)(64 + (r * 128 / ch)), v = (uint8_t)(64 + (x * 128 / W));
            if (r * sx >= H / 2) { u = (c & 1) ? 240 : 102; v = (c & 1) ? 118 : 240; } // blue / red columns
            cb[(size_t)r * cw + c] = u; cr[(size_t)r * cw + c] = v;
        }
}

static const char *fmt_name(int f) {
    switch (f) {
    case VK_FORMAT_G8_B8_R8_3PLANE_420_UNORM: return "G8_B8_R8_3PLANE_420";
    case VK_FORMAT_G8_B8_R8_3PLANE_444_UNORM: return "G8_B8_R8_3PLANE_444";
    case VK_FORMAT_G16_B16_R16_3PLANE_420_UNORM: return "G16_B16_R16_3PLANE_420";
    case VK_FORMAT_G16_B16_R16_3PLANE_444_UNORM: return "G16_B16_R16_3PLANE_444";
    }
    return "?";
}

// What vk_ycbcr_pipeline_get (vk_video_impl_windows.c) builds for this format.
static int sampler_ok(VkFormat fmt, int hdr) {
    VkFormatProperties fp; vkGetPhysicalDeviceFormatProperties(g_phys, fmt, &fp);
    VkFormatFeatureFlags ff = fp.optimalTilingFeatures;
    VkSamplerYcbcrConversionCreateInfo ci = { VK_STRUCTURE_TYPE_SAMPLER_YCBCR_CONVERSION_CREATE_INFO };
    ci.format = fmt;
    ci.ycbcrModel = hdr ? VK_SAMPLER_YCBCR_MODEL_CONVERSION_YCBCR_2020 : VK_SAMPLER_YCBCR_MODEL_CONVERSION_YCBCR_709;
    ci.ycbcrRange = VK_SAMPLER_YCBCR_RANGE_ITU_NARROW;
    ci.xChromaOffset = ci.yChromaOffset = (ff & VK_FORMAT_FEATURE_COSITED_CHROMA_SAMPLES_BIT) ? VK_CHROMA_LOCATION_COSITED_EVEN : VK_CHROMA_LOCATION_MIDPOINT;
    ci.chromaFilter = (ff & VK_FORMAT_FEATURE_SAMPLED_IMAGE_YCBCR_CONVERSION_LINEAR_FILTER_BIT) ? VK_FILTER_LINEAR : VK_FILTER_NEAREST;
    VkSamplerYcbcrConversion conv = VK_NULL_HANDLE;
    VkResult r = vkCreateSamplerYcbcrConversion(g_dev, &ci, NULL, &conv);
    if (conv) vkDestroySamplerYcbcrConversion(g_dev, conv, NULL);
    printf("  renderer sampler: %s (cosited %s, linear chroma %s)\n", r == VK_SUCCESS ? "ok" : "FAILED",
           (ff & VK_FORMAT_FEATURE_COSITED_CHROMA_SAMPLES_BIT) ? "yes" : "no",
           (ff & VK_FORMAT_FEATURE_SAMPLED_IMAGE_YCBCR_CONVERSION_LINEAR_FILTER_BIT) ? "yes" : "no");
    return r == VK_SUCCESS;
}

static int cmpd(const void *a, const void *b) { double x = *(const double *)a, y = *(const double *)b; return x < y ? -1 : x > y; }

static int run_mode(pyrowave_device penc, int c444, int hdr, int nframes, double mbps) {
    printf("\n== %s %s ==\n", c444 ? "4:4:4" : "4:2:0", hdr ? "HDR (16-bit planes)" : "SDR (8-bit planes)");
    if (!pyrowave_win_color_supported(c444, hdr)) { printf("  not supported on this GPU -- skipped\n"); return 1; }
    int cw = c444 ? W : W / 2, ch = c444 ? H : H / 2, bps = hdr ? 2 : 1;
    size_t ys = (size_t)W * H, cs = (size_t)cw * ch;
    pyrowave_encoder enc;
    pyrowave_encoder_create_info eci = { penc, W, H, c444 ? PYROWAVE_CHROMA_SUBSAMPLING_444 : PYROWAVE_CHROMA_SUBSAMPLING_420 };
    CHECK(pyrowave_encoder_create(&eci, &enc) == PYROWAVE_SUCCESS, "encoder");
    pyrowave_decoder_create_info dci = { penc, W, H, eci.chroma, false };
    pyrowave_decoder ref; CHECK(pyrowave_decoder_create(&dci, &ref) == PYROWAVE_SUCCESS, "ref decoder");
    uint8_t *yuv = malloc(ys + 2 * cs), *refp = malloc(ys + 2 * cs), *gpu = malloc((ys + 2 * cs) * bps);
    size_t budget = (size_t)(mbps * 1e6 / 8 / 60);
    uint8_t *bs = malloc(budget + 262144);
    pyrowave_rate_control rc = { budget };
    double *lat = malloc(sizeof(double) * nframes);
    int bad = 0, maxdiff = 0, nlat = 0;
    VkFormat fmt_seen = 0;
    for (int i = 0; i < nframes; i++) {
        make_frame(i, c444, yuv, yuv + ys, yuv + ys + cs);
        pyrowave_cpu_buffer cb = { 0 };
        cb.format = c444 ? PYROWAVE_CPU_BUFFER_FORMAT_YUV444P : PYROWAVE_CPU_BUFFER_FORMAT_YUV420P; cb.width = W; cb.height = H;
        cb.data[0] = yuv; cb.row_stride_in_bytes[0] = W; cb.plane_size_in_bytes[0] = ys;
        cb.data[1] = yuv + ys; cb.row_stride_in_bytes[1] = cw; cb.plane_size_in_bytes[1] = cs;
        cb.data[2] = yuv + ys + cs; cb.row_stride_in_bytes[2] = cw; cb.plane_size_in_bytes[2] = cs;
        CHECK(pyrowave_encoder_encode_cpu_synchronous(enc, &cb, &rc) == PYROWAVE_SUCCESS, "encode");
        size_t cap = budget + 262144, np = 0, outp = 0;
        CHECK(pyrowave_encoder_compute_num_packets(enc, cap, &np) == PYROWAVE_SUCCESS && np == 1, "one packet");
        pyrowave_packet pk;
        CHECK(pyrowave_encoder_packetize(enc, &pk, cap, &outp, bs, cap) == PYROWAVE_SUCCESS, "packetize");
        uint8_t *au = bs + pk.offset; size_t len = pk.size;
        // What rust-shine's take_access_unit stamps: limited range, left siting; HDR adds
        // BT.2020 primaries, PQ, BT.2020 matrix.
        au[7] |= 0x40 | 0x80;
        if (hdr) au[7] |= 0x08 | 0x10 | 0x20;
        if (c444) CHECK(au[7] & 0x04, "encoder set chroma_resolution 4:4:4");

        void *img, *sem, *slot; int vkfmt, w, h; uint64_t val;
        double t0 = now_ms();
        CHECK(pyrowave_win_decode(au, len, &img, &vkfmt, &sem, &val, &w, &h, &slot), "pyrowave_win_decode");
        wait_val((VkSemaphore)sem, val);
        lat[nlat++] = now_ms() - t0;
        fmt_seen = vkfmt;
        if (i < 8) {
            readback((VkImage)img, c444, bps, gpu);
            pyrowave_decoder_clear(ref);
            CHECK(pyrowave_decoder_push_packet(ref, au, len) == PYROWAVE_SUCCESS, "ref push");
            pyrowave_cpu_buffer rb = cb;
            rb.data[0] = refp; rb.data[1] = refp + ys; rb.data[2] = refp + ys + cs;
            CHECK(pyrowave_decoder_decode_cpu_buffer_synchronous(ref, &rb) == PYROWAVE_SUCCESS, "ref decode");
            int frame_bad = 0;
            for (size_t k = 0; k < ys + 2 * cs; k++) {
                int g = hdr ? (int)(((uint16_t *)gpu)[k] + 128) / 257 : gpu[k];
                int d = abs(g - (int)refp[k]);
                if (d > maxdiff) maxdiff = d;
                if (d > (hdr ? 1 : 0)) frame_bad++;
            }
            // 4:4:4 must keep the 1-pixel stripes: neighbouring Cr columns differ.
            if (c444) {
                size_t row = ys + cs + (size_t)(3 * H / 4) * W + W / 2;
                int a = hdr ? ((uint16_t *)gpu)[row] / 257 : gpu[row], b2 = hdr ? ((uint16_t *)gpu)[row + 1] / 257 : gpu[row + 1];
                if (abs(a - b2) < 60) { printf("  frame %d: stripes lost (Cr %d %d)\n", i, a, b2); frame_bad++; }
            }
            if (frame_bad) { printf("  frame %d: %d samples off the CPU decode\n", i, frame_bad); bad++; }
        }
        pyrowave_win_release_slot(slot);
    }
    qsort(lat, nlat, sizeof(double), cmpd);
    printf("  ring format %s; verified 8 frames vs CPU decode: %s (max diff %d%s)\n", fmt_name(fmt_seen), bad ? "MISMATCH" : "ok",
           maxdiff, hdr ? " in 8-bit steps" : "");
    printf("  decode latency over %d frames: p50 %.2f ms, p95 %.2f ms, max %.2f ms\n", nlat, lat[nlat / 2], lat[nlat * 95 / 100], lat[nlat - 1]);
    int sok = sampler_ok((VkFormat)fmt_seen, hdr);
    pyrowave_decoder_destroy(ref); pyrowave_encoder_destroy(enc);
    free(yuv); free(refp); free(gpu); free(bs); free(lat);
    return !bad && sok;
}

int main(int argc, char **argv) {
    W = argc > 1 ? atoi(argv[1]) : 3840; H = argc > 2 ? atoi(argv[2]) : 2160;
    int nframes = argc > 3 ? atoi(argv[3]) : 120; double mbps = argc > 4 ? atof(argv[4]) : 400;
    InitializeCriticalSection(&g_qlock);
    AVDictionary *o = NULL;
    av_dict_set(&o, "instance_extensions", "+VK_KHR_surface+VK_KHR_win32_surface+VK_EXT_swapchain_colorspace", 0);
    av_dict_set(&o, "device_extensions", "+VK_KHR_swapchain+VK_KHR_external_memory_win32+VK_KHR_external_semaphore_win32", 0);
    CHECK(av_hwdevice_ctx_create(&g_hw, AV_HWDEVICE_TYPE_VULKAN, getenv("BENCH_VKDEV"), o, 0) == 0, "renderer device");
    AVVulkanDeviceContext *vk = (AVVulkanDeviceContext *)((AVHWDeviceContext *)g_hw->data)->hwctx;
    g_dev = vk->act_dev; g_phys = vk->phys_dev;
    for (int i = 0; i < vk->nb_qf; i++) if (vk->qf[i].flags & VK_QUEUE_GRAPHICS_BIT) { g_qf = vk->qf[i].idx; break; }
    vkGetDeviceQueue(g_dev, g_qf, 0, &g_q);
    VkPhysicalDeviceIDProperties idp = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_ID_PROPERTIES };
    VkPhysicalDeviceProperties2 pp = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2 }; pp.pNext = &idp;
    vkGetPhysicalDeviceProperties2(g_phys, &pp);
    pyrowave_luid luid; memcpy(luid.luid, idp.deviceLUID, VK_LUID_SIZE);
    pyrowave_device penc;
    CHECK(pyrowave_create_device_by_compat(pp.properties.vendorID, pp.properties.deviceID, NULL, NULL,
                                           idp.deviceLUIDValid ? &luid : NULL, &penc) == PYROWAVE_SUCCESS, "encode device");
    printf("GPU: %s, %dx%d, %d frames per mode, %.0f Mbps at 60 fps\n", pp.properties.deviceName, W, H, nframes, mbps);
    int ok = 1;
    for (int hdr = 0; hdr < 2; hdr++)
        for (int c444 = 0; c444 < 2; c444++) ok &= run_mode(penc, c444, hdr, nframes, mbps);
    printf("\n%s\n", ok ? "ALL MODES OK" : "SOME MODES FAILED");
    return ok ? 0 : 1;
}
