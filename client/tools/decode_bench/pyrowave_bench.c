// pyrowave_bench: the Windows client's zero-copy PyroWave decode path, offline.
//
// - "Renderer" device: the same ffmpeg-created Vulkan device the client renders
//   with (vk_hwdev_bridge_windows.c), with external memory/semaphore win32.
// - PyroWave device: its own VkDevice on the same physical GPU (LUID match).
// - A ring of G8_B8_R8_3PLANE_420 images is allocated on the renderer device
//   with exportable memory (OPAQUE_WIN32) and imported into PyroWave, which
//   decodes straight into them (compute, R8 storage views of each plane).
// - A timeline semaphore exported from the renderer device is imported into
//   PyroWave; each decode signals the next value, the renderer waits on it.
//
// Test input is encoded here too, with PyroWave's own encoder, from a moving
// synthetic YUV420 pattern. BENCH_VERIFY=N checks the first N frames: planes
// read back through the renderer device must byte-match PyroWave's own CPU
// decode of the same access unit. Otherwise frames are paced at <fps> like
// network arrivals and latency (arrival -> renderer semaphore signalled) is
// measured.
//
// usage: pyrowave_bench <width> <height> <fps> <seconds> <mbps>   (BENCH_VKDEV picks the GPU)
// Build (MSYS2 UCRT64), after scripts/build_pyrowave_windows.sh:
//   gcc -O2 pyrowave_bench.c -o pyrowave_bench.exe -I../../third_party/pyrowave/vendor/pyrowave \
//       $(pkg-config --cflags --libs libavutil) -L../../third_party/pyrowave/build-win \
//       -lusbridge-pyrowave -lstdc++ -lvulkan-1 -lwinmm
#define VK_USE_PLATFORM_WIN32_KHR
#include <windows.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>
#include <stdint.h>
#include <math.h>
#include <libavutil/dict.h>
#include <vulkan/vulkan.h>
#include <vulkan/vulkan_win32.h>
#include <pyrowave.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_vulkan.h>

#define CHECK(x, msg) do { if (!(x)) { fprintf(stderr, "FAIL: %s (line %d)\n", msg, __LINE__); exit(1); } } while (0)
#define RING 4
#define MAXF 4096

static double now_ms(void) {
    static LARGE_INTEGER f; LARGE_INTEGER c;
    if (!f.QuadPart) QueryPerformanceFrequency(&f);
    QueryPerformanceCounter(&c);
    return (double)c.QuadPart * 1000.0 / (double)f.QuadPart;
}
static void sleep_until(double t) {
    for (;;) { double d = t - now_ms(); if (d <= 0) return; if (d > 2.0) Sleep((DWORD)(d - 1.5)); else SwitchToThread(); }
}

static int W, H;
static VkDevice g_dev; static VkPhysicalDevice g_phys; static uint32_t g_qf; static VkQueue g_q;
static VkImage g_img[RING]; static VkDeviceMemory g_mem[RING];
static pyrowave_image g_pimg[RING];
static pyrowave_gpu_buffers g_bufs[RING];
static VkSemaphore g_sem; static pyrowave_sync_object g_psync; static uint64_t g_val;
static pyrowave_device g_pdev; static pyrowave_decoder g_dec;
// BENCH_SHARED=1: PyroWave decodes on the renderer's own VkDevice (no memory
// sharing between devices) on a compute queue nobody else uses.
static int g_shared; static uint32_t g_cqf;
static pyrowave_device g_penc; // encoder device (always its own)
static CRITICAL_SECTION g_qlock;
static void q_lock(void *u) { (void)u; EnterCriticalSection(&g_qlock); }
static void q_unlock(void *u) { (void)u; LeaveCriticalSection(&g_qlock); }

static uint8_t *g_au[MAXF]; static size_t g_au_len[MAXF]; static int g_nau;
static double g_sched[MAXF], g_done[MAXF];

static uint32_t mem_type(uint32_t bits, VkMemoryPropertyFlags want) {
    VkPhysicalDeviceMemoryProperties mp; vkGetPhysicalDeviceMemoryProperties(g_phys, &mp);
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++)
        if ((bits & (1u << i)) && (mp.memoryTypes[i].propertyFlags & want) == want) return i;
    return UINT32_MAX;
}

static VkImageCreateInfo plane_image_info(VkExternalMemoryImageCreateInfo *emi) {
    VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
    ici.pNext = emi;
    ici.flags = VK_IMAGE_CREATE_MUTABLE_FORMAT_BIT | VK_IMAGE_CREATE_EXTENDED_USAGE_BIT;
    ici.imageType = VK_IMAGE_TYPE_2D; ici.format = VK_FORMAT_G8_B8_R8_3PLANE_420_UNORM;
    ici.extent.width = W; ici.extent.height = H; ici.extent.depth = 1;
    ici.mipLevels = 1; ici.arrayLayers = 1; ici.samples = VK_SAMPLE_COUNT_1_BIT;
    ici.tiling = VK_IMAGE_TILING_OPTIMAL;
    ici.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_STORAGE_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
    ici.sharingMode = VK_SHARING_MODE_EXCLUSIVE;
    ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
    return ici;
}

static int g_dedicated_only;
static void query_dedicated_only(void) {
    VkPhysicalDeviceExternalImageFormatInfo ext = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_EXTERNAL_IMAGE_FORMAT_INFO };
    ext.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_WIN32_BIT;
    VkPhysicalDeviceImageFormatInfo2 fi = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_IMAGE_FORMAT_INFO_2 };
    fi.pNext = &ext; fi.format = VK_FORMAT_G8_B8_R8_3PLANE_420_UNORM; fi.type = VK_IMAGE_TYPE_2D;
    fi.tiling = VK_IMAGE_TILING_OPTIMAL;
    fi.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_STORAGE_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
    fi.flags = VK_IMAGE_CREATE_MUTABLE_FORMAT_BIT | VK_IMAGE_CREATE_EXTENDED_USAGE_BIT;
    VkExternalImageFormatProperties ep = { VK_STRUCTURE_TYPE_EXTERNAL_IMAGE_FORMAT_PROPERTIES };
    VkImageFormatProperties2 ip = { VK_STRUCTURE_TYPE_IMAGE_FORMAT_PROPERTIES_2 }; ip.pNext = &ep;
    vkGetPhysicalDeviceImageFormatProperties2(g_phys, &fi, &ip);
    g_dedicated_only = !!(ep.externalMemoryProperties.externalMemoryFeatures & VK_EXTERNAL_MEMORY_FEATURE_DEDICATED_ONLY_BIT);
    if (getenv("BENCH_DEDICATED")) g_dedicated_only = atoi(getenv("BENCH_DEDICATED"));
}

static void setup_ring(void) {
    query_dedicated_only();
    HANDLE handles[RING];
    PFN_vkGetMemoryWin32HandleKHR getMem = (PFN_vkGetMemoryWin32HandleKHR)vkGetDeviceProcAddr(g_dev, "vkGetMemoryWin32HandleKHR");
    CHECK(getMem, "vkGetMemoryWin32HandleKHR");
    for (int s = 0; s < RING; s++) {
        VkExternalMemoryImageCreateInfo emi = { VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO };
        emi.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_WIN32_BIT;
        VkImageCreateInfo ici = plane_image_info(&emi);
        CHECK(vkCreateImage(g_dev, &ici, NULL, &g_img[s]) == VK_SUCCESS, "vkCreateImage");
        VkMemoryRequirements mr; vkGetImageMemoryRequirements(g_dev, g_img[s], &mr);
        VkMemoryDedicatedAllocateInfo ded = { VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO }; ded.image = g_img[s];
        VkExportMemoryAllocateInfo exp = { VK_STRUCTURE_TYPE_EXPORT_MEMORY_ALLOCATE_INFO };
        // Always dedicated: Granite (PyroWave) always imports external memory as
        // a dedicated allocation, and an OPAQUE import must match its export.
        exp.pNext = (g_dedicated_only || !getenv("BENCH_DEDICATED") || atoi(getenv("BENCH_DEDICATED"))) ? &ded : NULL;
        exp.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_WIN32_BIT;
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.pNext = &exp; mai.allocationSize = mr.size; mai.memoryTypeIndex = mem_type(mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
        CHECK(vkAllocateMemory(g_dev, &mai, NULL, &g_mem[s]) == VK_SUCCESS, "alloc exportable");
        CHECK(vkBindImageMemory(g_dev, g_img[s], g_mem[s], 0) == VK_SUCCESS, "bind");
        VkMemoryGetWin32HandleInfoKHR gh = { VK_STRUCTURE_TYPE_MEMORY_GET_WIN32_HANDLE_INFO_KHR };
        gh.memory = g_mem[s]; gh.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_WIN32_BIT;
        CHECK(getMem(g_dev, &gh, &handles[s]) == VK_SUCCESS, "export memory");

    }
    // Put every image in GENERAL and release it to the external queue family
    // once, before PyroWave ever acquires it: PyroWave assumes external images
    // are in GENERAL, and a never-transitioned image (UNDEFINED, compression
    // metadata uninitialized) came out with a garbage plane on NVIDIA.
    if (!getenv("BENCH_NO_INIT_LAYOUT")) {
        VkCommandPoolCreateInfo pci = { VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO }; pci.queueFamilyIndex = g_qf;
        VkCommandPool pool; vkCreateCommandPool(g_dev, &pci, NULL, &pool);
        VkCommandBufferAllocateInfo cai = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO }; cai.commandPool = pool; cai.commandBufferCount = 1;
        VkCommandBuffer cb; vkAllocateCommandBuffers(g_dev, &cai, &cb);
        VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO }; vkBeginCommandBuffer(cb, &bi);
        VkImageMemoryBarrier b[RING];
        for (int s = 0; s < RING; s++) {
            VkImageMemoryBarrier x = { VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER };
            x.oldLayout = VK_IMAGE_LAYOUT_UNDEFINED; x.newLayout = VK_IMAGE_LAYOUT_GENERAL;
            x.srcQueueFamilyIndex = g_qf; x.dstQueueFamilyIndex = VK_QUEUE_FAMILY_EXTERNAL; x.image = g_img[s];
            x.subresourceRange.aspectMask = VK_IMAGE_ASPECT_PLANE_0_BIT | VK_IMAGE_ASPECT_PLANE_1_BIT | VK_IMAGE_ASPECT_PLANE_2_BIT;
            x.subresourceRange.levelCount = 1; x.subresourceRange.layerCount = 1;
            b[s] = x;
        }
        vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT, 0, 0, NULL, 0, NULL, RING, b);
        vkEndCommandBuffer(cb);
        VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO }; si.commandBufferCount = 1; si.pCommandBuffers = &cb;
        CHECK(vkQueueSubmit(g_q, 1, &si, VK_NULL_HANDLE) == VK_SUCCESS, "init layout");
        vkQueueWaitIdle(g_q);
        vkDestroyCommandPool(g_dev, pool, NULL);
    }
    int rev = getenv("BENCH_IMPORT_REVERSE") != NULL;
    for (int k = 0; k < RING; k++) {
        int s = rev ? RING - 1 - k : k;
        VkExternalMemoryImageCreateInfo emi2 = { VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO };
        emi2.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_WIN32_BIT;
        VkImageCreateInfo ici2 = plane_image_info(&emi2);
        pyrowave_image_create_info pi = { 0 };
        pi.device = g_pdev; pi.external_handle = (pyrowave_os_handle)handles[s];
        pi.handle_type = VK_EXTERNAL_MEMORY_HANDLE_TYPE_OPAQUE_WIN32_BIT; pi.image_create_info = &ici2;
        CHECK(pyrowave_image_create(&pi, &g_pimg[s]) == PYROWAVE_SUCCESS, "pyrowave_image_create");
        for (int p = 0; p < 3; p++)
            CHECK(pyrowave_image_get_image_view(g_pimg[s], (VkImageAspectFlagBits)(VK_IMAGE_ASPECT_PLANE_0_BIT << p),
                                                VK_IMAGE_USAGE_STORAGE_BIT, &g_bufs[s].planes[p]) == PYROWAVE_SUCCESS, "plane view");
    }
    // timeline semaphore: renderer side exports, PyroWave imports
    VkExportSemaphoreCreateInfo es = { VK_STRUCTURE_TYPE_EXPORT_SEMAPHORE_CREATE_INFO };
    es.handleTypes = VK_EXTERNAL_SEMAPHORE_HANDLE_TYPE_OPAQUE_WIN32_BIT;
    VkSemaphoreTypeCreateInfo st = { VK_STRUCTURE_TYPE_SEMAPHORE_TYPE_CREATE_INFO };
    st.pNext = &es; st.semaphoreType = VK_SEMAPHORE_TYPE_TIMELINE;
    VkSemaphoreCreateInfo sci = { VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO }; sci.pNext = &st;
    CHECK(vkCreateSemaphore(g_dev, &sci, NULL, &g_sem) == VK_SUCCESS, "timeline semaphore");
    PFN_vkGetSemaphoreWin32HandleKHR getSem = (PFN_vkGetSemaphoreWin32HandleKHR)vkGetDeviceProcAddr(g_dev, "vkGetSemaphoreWin32HandleKHR");
    VkSemaphoreGetWin32HandleInfoKHR gs = { VK_STRUCTURE_TYPE_SEMAPHORE_GET_WIN32_HANDLE_INFO_KHR };
    gs.semaphore = g_sem; gs.handleType = VK_EXTERNAL_SEMAPHORE_HANDLE_TYPE_OPAQUE_WIN32_BIT;
    HANDLE sh = NULL; CHECK(getSem && getSem(g_dev, &gs, &sh) == VK_SUCCESS, "export semaphore");
    pyrowave_sync_object_create_info si = { 0 };
    si.device = g_pdev; si.external_handle = (pyrowave_os_handle)sh;
    si.handle_type = VK_EXTERNAL_SEMAPHORE_HANDLE_TYPE_OPAQUE_WIN32_BIT; si.semaphore_type = VK_SEMAPHORE_TYPE_TIMELINE;
    CHECK(pyrowave_sync_object_create(&si, &g_psync) == PYROWAVE_SUCCESS, "import semaphore into PyroWave");
}

// Decode one AU into ring slot s; returns the timeline value that signals completion (0 = failed).
static uint64_t gpu_decode(int i, int s) {
    pyrowave_decoder_clear(g_dec);
    if (pyrowave_decoder_push_packet(g_dec, g_au[i], g_au_len[i]) != PYROWAVE_SUCCESS) return 0;
    if (!pyrowave_decoder_decode_is_ready(g_dec, false)) return 0;
    if (g_shared) {
        uint64_t v = ++g_val;
        pyrowave_gpu_sync_operation rel = { NULL, 0, { g_sem, v } };
        if (pyrowave_decoder_decode_gpu_buffer(g_dec, NULL, &rel, &g_bufs[s]) != PYROWAVE_SUCCESS) return 0;
        return v;
    }
    pyrowave_gpu_external_reference ref = { g_pimg[s], VK_QUEUE_FAMILY_EXTERNAL };
    pyrowave_gpu_sync_operation acq = { &ref, 1, { VK_NULL_HANDLE, 0 } };
    uint64_t v = ++g_val;
    pyrowave_gpu_sync_operation rel = { &ref, 1, { pyrowave_sync_object_get_semaphore(g_psync), v } };
    if (pyrowave_decoder_decode_gpu_buffer(g_dec, &acq, &rel, &g_bufs[s]) != PYROWAVE_SUCCESS) return 0;
    return v;
}

static void wait_val(uint64_t v) {
    VkSemaphoreWaitInfo wi = { VK_STRUCTURE_TYPE_SEMAPHORE_WAIT_INFO };
    wi.semaphoreCount = 1; wi.pSemaphores = &g_sem; wi.pValues = &v;
    CHECK(vkWaitSemaphores(g_dev, &wi, 2000000000ULL) == VK_SUCCESS, "vkWaitSemaphores");
}

// Read slot s's three planes back through the renderer device.
static void readback(int s, uint8_t *out) {
    size_t ys = (size_t)W * H, cs = (size_t)(W / 2) * (H / 2);
    VkBufferCreateInfo bci = { VK_STRUCTURE_TYPE_BUFFER_CREATE_INFO }; bci.size = ys + 2 * cs; bci.usage = VK_BUFFER_USAGE_TRANSFER_DST_BIT;
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
    b.oldLayout = VK_IMAGE_LAYOUT_GENERAL; b.newLayout = VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL;
    b.srcQueueFamilyIndex = g_shared ? VK_QUEUE_FAMILY_IGNORED : VK_QUEUE_FAMILY_EXTERNAL;
    b.dstQueueFamilyIndex = g_shared ? VK_QUEUE_FAMILY_IGNORED : g_qf; b.image = g_img[s];
    b.subresourceRange.aspectMask = VK_IMAGE_ASPECT_PLANE_0_BIT | VK_IMAGE_ASPECT_PLANE_1_BIT | VK_IMAGE_ASPECT_PLANE_2_BIT;
    b.subresourceRange.levelCount = 1; b.subresourceRange.layerCount = 1; b.dstAccessMask = VK_ACCESS_TRANSFER_READ_BIT;
    vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_TRANSFER_BIT, 0, 0, NULL, 0, NULL, 1, &b);
    VkBufferImageCopy r[3] = { 0 };
    for (int p = 0; p < 3; p++) {
        r[p].bufferOffset = p == 0 ? 0 : ys + (p - 1) * cs;
        r[p].imageSubresource.aspectMask = VK_IMAGE_ASPECT_PLANE_0_BIT << p; r[p].imageSubresource.layerCount = 1;
        r[p].imageExtent.width = p ? W / 2 : W; r[p].imageExtent.height = p ? H / 2 : H; r[p].imageExtent.depth = 1;
    }
    vkCmdCopyImageToBuffer(cb, g_img[s], VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL, buf, 3, r);
    VkImageMemoryBarrier rb = b;
    rb.oldLayout = VK_IMAGE_LAYOUT_TRANSFER_SRC_OPTIMAL; rb.newLayout = VK_IMAGE_LAYOUT_GENERAL;
    rb.srcQueueFamilyIndex = g_shared ? VK_QUEUE_FAMILY_IGNORED : g_qf;
    rb.dstQueueFamilyIndex = g_shared ? VK_QUEUE_FAMILY_IGNORED : VK_QUEUE_FAMILY_EXTERNAL;
    rb.srcAccessMask = VK_ACCESS_TRANSFER_READ_BIT; rb.dstAccessMask = 0;
    vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TRANSFER_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT, 0, 0, NULL, 0, NULL, 1, &rb);
    vkEndCommandBuffer(cb);
    VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO }; si.commandBufferCount = 1; si.pCommandBuffers = &cb;
    q_lock(NULL);
    CHECK(vkQueueSubmit(g_q, 1, &si, VK_NULL_HANDLE) == VK_SUCCESS, "submit readback");
    vkQueueWaitIdle(g_q);
    q_unlock(NULL);
    void *p; vkMapMemory(g_dev, bm, 0, ys + 2 * cs, 0, &p); memcpy(out, p, ys + 2 * cs); vkUnmapMemory(g_dev, bm);
    vkDestroyCommandPool(g_dev, pool, NULL); vkDestroyBuffer(g_dev, buf, NULL); vkFreeMemory(g_dev, bm, NULL);
}

// Synthetic moving content: gradients, a moving box, some texture.
static void make_frame(int i, uint8_t *y, uint8_t *cb, uint8_t *cr) {
    for (int r = 0; r < H; r++)
        for (int c = 0; c < W; c++) {
            int v = ((c + i * 7) ^ (r * 3)) & 255;
            int inbox = c > (i * 13) % W && c < (i * 13) % W + W / 6 && r > H / 3 && r < H / 2;
            y[(size_t)r * W + c] = (uint8_t)(inbox ? 235 : 16 + (v * 219) / 255);
        }
    for (int r = 0; r < H / 2; r++)
        for (int c = 0; c < W / 2; c++) {
            cb[(size_t)r * (W / 2) + c] = (uint8_t)(64 + ((c + i) & 127));
            cr[(size_t)r * (W / 2) + c] = (uint8_t)(64 + ((r + 2 * i) & 127));
        }
}

typedef struct { uint64_t v; int idx; } RItem;
static RItem rq[256]; static volatile LONG rh, rt;
static volatile LONG g_dec_done;
static DWORD WINAPI render_thread(LPVOID a) {
    (void)a;
    for (;;) {
        if (rh == rt) { if (g_dec_done) break; SwitchToThread(); continue; }
        RItem it = rq[rh % 256];
        wait_val(it.v);
        g_done[it.idx] = now_ms();
        InterlockedIncrement(&rh);
    }
    return 0;
}

static void setup_ring_shared(void) {
    uint32_t fams[2] = { g_qf, g_cqf };
    for (int s = 0; s < RING; s++) {
        VkImageCreateInfo ici = plane_image_info(NULL);
        ici.sharingMode = g_qf != g_cqf ? VK_SHARING_MODE_CONCURRENT : VK_SHARING_MODE_EXCLUSIVE;
        ici.queueFamilyIndexCount = g_qf != g_cqf ? 2 : 0; ici.pQueueFamilyIndices = fams;
        CHECK(vkCreateImage(g_dev, &ici, NULL, &g_img[s]) == VK_SUCCESS, "vkCreateImage (shared)");
        VkMemoryRequirements mr; vkGetImageMemoryRequirements(g_dev, g_img[s], &mr);
        VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
        mai.allocationSize = mr.size; mai.memoryTypeIndex = mem_type(mr.memoryTypeBits, VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT);
        CHECK(vkAllocateMemory(g_dev, &mai, NULL, &g_mem[s]) == VK_SUCCESS, "alloc");
        CHECK(vkBindImageMemory(g_dev, g_img[s], g_mem[s], 0) == VK_SUCCESS, "bind");
        for (int pl = 0; pl < 3; pl++) {
            pyrowave_image_view *v = &g_bufs[s].planes[pl];
            memset(v, 0, sizeof(*v));
            v->image = g_img[s]; v->width = W; v->height = H;
            v->image_format = VK_FORMAT_G8_B8_R8_3PLANE_420_UNORM; v->view_format = VK_FORMAT_R8_UNORM;
            v->aspect = (VkImageAspectFlagBits)(VK_IMAGE_ASPECT_PLANE_0_BIT << pl);
            v->swizzle = VK_COMPONENT_SWIZZLE_IDENTITY; v->layout = VK_IMAGE_LAYOUT_GENERAL;
        }
    }
    // UNDEFINED -> GENERAL once; the images stay in GENERAL from then on.
    VkCommandPoolCreateInfo pci = { VK_STRUCTURE_TYPE_COMMAND_POOL_CREATE_INFO }; pci.queueFamilyIndex = g_qf;
    VkCommandPool pool; vkCreateCommandPool(g_dev, &pci, NULL, &pool);
    VkCommandBufferAllocateInfo cai = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_ALLOCATE_INFO }; cai.commandPool = pool; cai.commandBufferCount = 1;
    VkCommandBuffer cb; vkAllocateCommandBuffers(g_dev, &cai, &cb);
    VkCommandBufferBeginInfo bi = { VK_STRUCTURE_TYPE_COMMAND_BUFFER_BEGIN_INFO }; vkBeginCommandBuffer(cb, &bi);
    VkImageMemoryBarrier b[RING];
    for (int s = 0; s < RING; s++) {
        VkImageMemoryBarrier x = { VK_STRUCTURE_TYPE_IMAGE_MEMORY_BARRIER };
        x.oldLayout = VK_IMAGE_LAYOUT_UNDEFINED; x.newLayout = VK_IMAGE_LAYOUT_GENERAL;
        x.srcQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED; x.dstQueueFamilyIndex = VK_QUEUE_FAMILY_IGNORED; x.image = g_img[s];
        x.subresourceRange.aspectMask = VK_IMAGE_ASPECT_PLANE_0_BIT | VK_IMAGE_ASPECT_PLANE_1_BIT | VK_IMAGE_ASPECT_PLANE_2_BIT;
        x.subresourceRange.levelCount = 1; x.subresourceRange.layerCount = 1;
        b[s] = x;
    }
    vkCmdPipelineBarrier(cb, VK_PIPELINE_STAGE_TOP_OF_PIPE_BIT, VK_PIPELINE_STAGE_BOTTOM_OF_PIPE_BIT, 0, 0, NULL, 0, NULL, RING, b);
    vkEndCommandBuffer(cb);
    VkSubmitInfo si = { VK_STRUCTURE_TYPE_SUBMIT_INFO }; si.commandBufferCount = 1; si.pCommandBuffers = &cb;
    q_lock(NULL);
    CHECK(vkQueueSubmit(g_q, 1, &si, VK_NULL_HANDLE) == VK_SUCCESS, "init layout");
    vkQueueWaitIdle(g_q);
    q_unlock(NULL);
    vkDestroyCommandPool(g_dev, pool, NULL);
    VkSemaphoreTypeCreateInfo st = { VK_STRUCTURE_TYPE_SEMAPHORE_TYPE_CREATE_INFO }; st.semaphoreType = VK_SEMAPHORE_TYPE_TIMELINE;
    VkSemaphoreCreateInfo sci = { VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO }; sci.pNext = &st;
    CHECK(vkCreateSemaphore(g_dev, &sci, NULL, &g_sem) == VK_SUCCESS, "timeline semaphore");
}

static int cmpd(const void *a, const void *b) { double x = *(double *)a, y = *(double *)b; return x < y ? -1 : x > y; }

int main(int argc, char **argv) {
    W = argc > 1 ? atoi(argv[1]) : 3840; H = argc > 2 ? atoi(argv[2]) : 2160;
    double fps = argc > 3 ? atof(argv[3]) : 120, secs = argc > 4 ? atof(argv[4]) : 5, mbps = argc > 5 ? atof(argv[5]) : 400;
    timeBeginPeriod(1);

    AVDictionary *o = NULL;
    av_dict_set(&o, "instance_extensions", "+VK_KHR_surface+VK_KHR_win32_surface+VK_EXT_swapchain_colorspace", 0);
    av_dict_set(&o, "device_extensions", "+VK_KHR_swapchain+VK_KHR_external_memory_win32+VK_KHR_external_semaphore_win32", 0);
    AVBufferRef *hw = NULL;
    CHECK(av_hwdevice_ctx_create(&hw, AV_HWDEVICE_TYPE_VULKAN, getenv("BENCH_VKDEV"), o, 0) == 0, "renderer device");
    AVVulkanDeviceContext *vk = (AVVulkanDeviceContext *)((AVHWDeviceContext *)hw->data)->hwctx;
    g_dev = vk->act_dev; g_phys = vk->phys_dev;
    for (int i = 0; i < vk->nb_qf; i++) if (vk->qf[i].flags & VK_QUEUE_GRAPHICS_BIT) { g_qf = vk->qf[i].idx; break; }
    vkGetDeviceQueue(g_dev, g_qf, 0, &g_q);
    VkPhysicalDeviceIDProperties idp = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_ID_PROPERTIES };
    VkPhysicalDeviceProperties2 pp = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2 }; pp.pNext = &idp;
    vkGetPhysicalDeviceProperties2(g_phys, &pp);
    pyrowave_luid luid; memcpy(luid.luid, idp.deviceLUID, VK_LUID_SIZE);
    InitializeCriticalSection(&g_qlock);
    CHECK(pyrowave_create_device_by_compat(pp.properties.vendorID, pp.properties.deviceID, NULL, NULL,
                                           idp.deviceLUIDValid ? &luid : NULL, &g_penc) == PYROWAVE_SUCCESS, "pyrowave encode device");
    g_pdev = g_penc;
    g_shared = getenv("BENCH_SHARED") != NULL;
    // Rebuilt create infos of ffmpeg's device: PyroWave reads them to learn
    // which features/extensions are on. Static: must outlive the device.
    static VkDeviceQueueCreateInfo qci[64];
    static float prio[64];
    static VkDeviceCreateInfo dci_dev;
    static VkInstanceCreateInfo ici_inst;
    static VkApplicationInfo app;
    static pyrowave_device_create_queue_info pq;
    if (g_shared) {
        for (int k = 0; k < 64; k++) prio[k] = 1.0f;
        g_cqf = g_qf;
        for (int i = 0; i < vk->nb_qf; i++) {
            qci[i] = (VkDeviceQueueCreateInfo){ VK_STRUCTURE_TYPE_DEVICE_QUEUE_CREATE_INFO };
            qci[i].queueFamilyIndex = vk->qf[i].idx; qci[i].queueCount = vk->qf[i].num; qci[i].pQueuePriorities = prio;
            if ((vk->qf[i].flags & VK_QUEUE_COMPUTE_BIT) && !(vk->qf[i].flags & VK_QUEUE_GRAPHICS_BIT)) g_cqf = vk->qf[i].idx;
        }
        dci_dev = (VkDeviceCreateInfo){ VK_STRUCTURE_TYPE_DEVICE_CREATE_INFO };
        dci_dev.pNext = &vk->device_features;
        dci_dev.queueCreateInfoCount = vk->nb_qf; dci_dev.pQueueCreateInfos = qci;
        dci_dev.enabledExtensionCount = vk->nb_enabled_dev_extensions; dci_dev.ppEnabledExtensionNames = vk->enabled_dev_extensions;
        app = (VkApplicationInfo){ VK_STRUCTURE_TYPE_APPLICATION_INFO }; app.apiVersion = VK_API_VERSION_1_3;
        ici_inst = (VkInstanceCreateInfo){ VK_STRUCTURE_TYPE_INSTANCE_CREATE_INFO };
        ici_inst.pApplicationInfo = &app;
        ici_inst.enabledExtensionCount = vk->nb_enabled_inst_extensions; ici_inst.ppEnabledExtensionNames = vk->enabled_inst_extensions;
        VkQueue cq; vkGetDeviceQueue(g_dev, g_cqf, 0, &cq);
        pq = (pyrowave_device_create_queue_info){ cq, g_cqf, 0 };
        pyrowave_device_create_info pci = { 0 };
        pci.GetInstanceProcAddr = vkGetInstanceProcAddr;
        pci.instance = vk->inst; pci.physical_device = g_phys; pci.device = g_dev;
        pci.instance_create_info = &ici_inst; pci.device_create_info = &dci_dev;
        pci.queue_info = &pq; pci.queue_info_count = 1;
        pci.queue_lock_callback = q_lock; pci.queue_unlock_callback = q_unlock;
        pyrowave_result pr = pyrowave_create_device(&pci, &g_pdev);
        printf("shared device: pyrowave_create_device -> %d (compute family %u, graphics family %u)\n", (int)pr, g_cqf, g_qf);
        CHECK(pr == PYROWAVE_SUCCESS, "pyrowave_create_device (shared)");
        pyrowave_device_set_queue_type(g_pdev, VK_QUEUE_COMPUTE_BIT);
    }
    printf("GPU: %s, %dx%d @%.0f, %.0f Mbps\n", pp.properties.deviceName, W, H, fps, mbps);

    // encode test frames
    int nframes = (int)(fps * secs); if (nframes > MAXF) nframes = MAXF;
    pyrowave_encoder enc;
    pyrowave_encoder_create_info eci = { g_penc, W, H, PYROWAVE_CHROMA_SUBSAMPLING_420 };
    CHECK(pyrowave_encoder_create(&eci, &enc) == PYROWAVE_SUCCESS, "encoder");
    size_t ys = (size_t)W * H, cs = (size_t)(W / 2) * (H / 2);
    uint8_t *yuv = malloc(ys + 2 * cs);
    pyrowave_rate_control rc = { (size_t)(mbps * 1e6 / 8 / fps) };
    size_t total = 0; double enc_ms = 0;
    for (int i = 0; i < nframes; i++) {
        make_frame(i, yuv, yuv + ys, yuv + ys + cs);
        pyrowave_cpu_buffer cb = { 0 };
        cb.format = PYROWAVE_CPU_BUFFER_FORMAT_YUV420P; cb.width = W; cb.height = H;
        cb.data[0] = yuv; cb.row_stride_in_bytes[0] = W; cb.plane_size_in_bytes[0] = ys;
        cb.data[1] = yuv + ys; cb.row_stride_in_bytes[1] = W / 2; cb.plane_size_in_bytes[1] = cs;
        cb.data[2] = yuv + ys + cs; cb.row_stride_in_bytes[2] = W / 2; cb.plane_size_in_bytes[2] = cs;
        double t = now_ms();
        CHECK(pyrowave_encoder_encode_cpu_synchronous(enc, &cb, &rc) == PYROWAVE_SUCCESS, "encode");
        size_t np = 0; CHECK(pyrowave_encoder_compute_num_packets(enc, 1400, &np) == PYROWAVE_SUCCESS, "num packets");
        pyrowave_packet *pk = malloc(sizeof(*pk) * np);
        uint8_t *bs = malloc(rc.maximum_bitstream_size + 65536);
        size_t outp = 0;
        CHECK(pyrowave_encoder_packetize(enc, pk, 1400, &outp, bs, rc.maximum_bitstream_size + 65536) == PYROWAVE_SUCCESS, "packetize");
        enc_ms += now_ms() - t;
        g_au_len[i] = pk[outp - 1].offset + pk[outp - 1].size;
        g_au[i] = bs; total += g_au_len[i];
        free(pk);
    }
    printf("encoded %d frames, avg %.0f KB/frame (%.0f Mbps at %.0f fps), encode %.2f ms/frame\n",
           nframes, total / 1024.0 / nframes, total * 8.0 * fps / nframes / 1e6, fps, enc_ms / nframes);
    g_nau = nframes;

    pyrowave_decoder_create_info dci = { g_pdev, W, H, PYROWAVE_CHROMA_SUBSAMPLING_420, false };
    CHECK(pyrowave_decoder_create(&dci, &g_dec) == PYROWAVE_SUCCESS, "decoder");
    pyrowave_decoder ref = NULL;
    if (getenv("BENCH_VERIFY") && !getenv("BENCH_REF_LATE"))
        CHECK(pyrowave_decoder_create(&dci, &ref) == PYROWAVE_SUCCESS, "ref decoder");
    if (g_shared) setup_ring_shared(); else setup_ring();

    if (getenv("BENCH_VERIFY")) {
        int n = atoi(getenv("BENCH_VERIFY")), bad = 0;
        if (!ref) CHECK(pyrowave_decoder_create(&dci, &ref) == PYROWAVE_SUCCESS, "ref decoder");
        uint8_t *a = malloc(ys + 2 * cs), *b = malloc(ys + 2 * cs);
        for (int i = 0; i < n && i < g_nau; i++) {
            int s = (i + (getenv("BENCH_SLOTSHIFT") ? atoi(getenv("BENCH_SLOTSHIFT")) : 0)) % RING;
            uint64_t v = gpu_decode(i, s); CHECK(v, "gpu decode");
            wait_val(v);
            readback(s, a);
            pyrowave_decoder_clear(ref);
            pyrowave_decoder_push_packet(ref, g_au[i], g_au_len[i]);
            pyrowave_cpu_buffer cb = { 0 };
            cb.format = PYROWAVE_CPU_BUFFER_FORMAT_YUV420P; cb.width = W; cb.height = H;
            cb.data[0] = b; cb.row_stride_in_bytes[0] = W; cb.plane_size_in_bytes[0] = ys;
            cb.data[1] = b + ys; cb.row_stride_in_bytes[1] = W / 2; cb.plane_size_in_bytes[1] = cs;
            cb.data[2] = b + ys + cs; cb.row_stride_in_bytes[2] = W / 2; cb.plane_size_in_bytes[2] = cs;
            CHECK(pyrowave_decoder_decode_cpu_buffer_synchronous(ref, &cb) == PYROWAVE_SUCCESS, "ref decode");
            size_t d = 0; for (size_t k = 0; k < ys + 2 * cs; k++) d += a[k] != b[k];
            // quality vs the encoder input
            make_frame(i, yuv, yuv + ys, yuv + ys + cs);
            double se = 0; for (size_t k = 0; k < ys; k++) { double e = (double)a[k] - yuv[k]; se += e * e; }
            double psnr = se > 0 ? 10 * log10(255.0 * 255.0 / (se / ys)) : 99;
            if (d) bad++;
            if (i < 3 || d) {
                double my = 0, ry = 0, mc = 0;
                for (size_t k = 0; k < ys; k += 97) { my += a[k]; ry += b[k]; }
                for (size_t k = ys; k < ys + 2 * cs; k += 97) mc += a[k];
                printf("frame %d slot %d: %zu bytes differ from CPU decode, luma PSNR vs source %.1f dB (mean Y gpu %.1f ref %.1f, mean C gpu %.1f)\n",
                       i, s, d, psnr, my / (ys / 97.0), ry / (ys / 97.0), mc / (2 * cs / 97.0));
            }
        }
        printf("verify: %d of %d frames mismatched\n", bad, n);
        return bad ? 1 : 0;
    }

    // paced run: arrivals at fps, decode immediately (async), renderer waits
    HANDLE rth = CreateThread(NULL, 0, render_thread, NULL, 0, NULL);
    FILETIME c0, e0, k0, u0, k1, u1;
    GetProcessTimes(GetCurrentProcess(), &c0, &e0, &k0, &u0);
    double t0 = now_ms() + 50, w0 = now_ms(), iv = 1000.0 / fps, submit_ms = 0;
    int fails = 0;
    for (int i = 0; i < g_nau; i++) {
        g_sched[i] = t0 + i * iv;
        sleep_until(g_sched[i]);
        while (rt - rh >= RING - 1) SwitchToThread(); // don't overwrite a slot not yet consumed
        double t = now_ms();
        uint64_t v = gpu_decode(i, i % RING);
        submit_ms += now_ms() - t;
        if (!v) { fails++; continue; }
        rq[rt % 256] = (RItem){ v, i };
        InterlockedIncrement(&rt);
    }
    InterlockedExchange(&g_dec_done, 1);
    WaitForSingleObject(rth, INFINITE);
    double wall = now_ms() - w0;
    GetProcessTimes(GetCurrentProcess(), &c0, &e0, &k1, &u1);
    ULARGE_INTEGER a1, b1, c1, d1;
    a1.LowPart = k0.dwLowDateTime; a1.HighPart = k0.dwHighDateTime; b1.LowPart = k1.dwLowDateTime; b1.HighPart = k1.dwHighDateTime;
    c1.LowPart = u0.dwLowDateTime; c1.HighPart = u0.dwHighDateTime; d1.LowPart = u1.dwLowDateTime; d1.HighPart = u1.dwHighDateTime;
    double cpu = ((b1.QuadPart - a1.QuadPart) + (d1.QuadPart - c1.QuadPart)) / 10000.0;
    static double lat[MAXF]; int n = 0;
    for (int i = 10; i < g_nau; i++) if (g_done[i] > 0) lat[n++] = g_done[i] - g_sched[i];
    qsort(lat, n, sizeof(double), cmpd);
    printf("{\"mode\":\"pyrowave-gpu\",\"frames\":%d,\"done\":%d,\"fails\":%d,\"lat_p50\":%.2f,\"lat_p99\":%.2f,\"lat_max\":%.2f,"
           "\"submit_ms\":%.3f,\"cpu_pct_of_one_core\":%.1f}\n",
           g_nau, n, fails, n ? lat[n / 2] : 0, n ? lat[(int)(n * 0.99)] : 0, n ? lat[n - 1] : 0, submit_ms / g_nau, 100.0 * cpu / wall);
    return 0;
}
