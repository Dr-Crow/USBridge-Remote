// d3d11_interop_windows.c — D3D11VA decode feeding the Vulkan renderer.
//
// D3D11VA is what moonlight-qt decodes with on Windows, and on a Radeon 780M
// it decodes 4K HEVC at ~232 fps with one thread, against ~151 fps for
// ffmpeg's Vulkan Video decoder (docs/WINDOWS_DECODE_PIPELINE.md). Our
// renderer is Vulkan, so each decoded frame takes one GPU-side copy:
//
//   D3D11VA decoder array slice --CopySubresourceRegion--> shared NV12/P010
//   texture (ring of D3DX_RING) --imported as VkImage--> vk_video_impl
//
// ordered by a shared ID3D11Fence that Vulkan imports as a timeline
// semaphore: the decode thread signals value N after the copy, the render
// thread waits for N before sampling. The copy is a few hundred
// microseconds of GPU time, and the renderer reads its own image, never one
// libavcodec still uses as a reference.
//
// The D3D11 device is created on the adapter whose LUID matches the shared
// Vulkan device (vk_hwdev_bridge_windows.c), and lives as long as that
// device does: the process. The ring is recreated when the frame size or
// format changes.
//
// Own translation unit for the same cgo "multiple definition" reason as
// vk_hwdev_bridge_windows.c.

#ifdef _WIN32

#define COBJMACROS
#define INITGUID
#define VK_USE_PLATFORM_WIN32_KHR
#include <windows.h>
#include <d3d11_4.h>
#include <dxgi1_2.h>
#include <vulkan/vulkan.h>
#include <vulkan/vulkan_win32.h>
#include <libavcodec/avcodec.h>
#include <libavutil/hwcontext.h>
#include <libavutil/hwcontext_d3d11va.h>
#include <libavutil/hwcontext_vulkan.h>
#include <stdio.h>
#include <stdlib.h>
#include <string.h>

extern void goVTLog(char *msg);
extern AVBufferRef *win_vk_hwdev_ctx_ref(void);
extern void vk_video_forget_image(void *vk_image);

#define D3DX_RING 4

static CRITICAL_SECTION g_dx_cs;
static int g_dx_cs_init = 0;
static int g_dx_tried = 0;
static AVBufferRef *g_dx_hwdev = NULL;          // ffmpeg D3D11VA device wrapping g_dx_dev
static AVD3D11VADeviceContext *g_dx_avctx = NULL;
static ID3D11Device5 *g_dx_dev = NULL;
static ID3D11DeviceContext4 *g_dx_ctx = NULL;
static ID3D11Fence *g_dx_fence = NULL;
static UINT64 g_dx_fence_val = 0;

static VkDevice g_dx_vkdev = VK_NULL_HANDLE;
static VkPhysicalDevice g_dx_vkphys = VK_NULL_HANDLE;
static VkSemaphore g_dx_vksem = VK_NULL_HANDLE;

typedef struct {
    ID3D11Texture2D *tex;
    VkImage img;
    VkDeviceMemory mem;
    volatile LONG busy; // handed to the renderer, not yet released
} DxSlot;
typedef struct {
    DxSlot slot[D3DX_RING];
    int w, h;
    DXGI_FORMAT fmt;
    int next;
} DxRing;
// g_dx_cur is the ring in use. A size/format change retires it to
// g_dx_old instead of destroying it, since the renderer may still hold one
// of its slots; it's freed once none are busy.
static DxRing *g_dx_cur = NULL;
static DxRing *g_dx_old = NULL;

static void dx_log(const char *m) { goVTLog((char *)m); }

static uint32_t dx_mem_type(uint32_t bits) {
    VkPhysicalDeviceMemoryProperties mp;
    vkGetPhysicalDeviceMemoryProperties(g_dx_vkphys, &mp);
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++)
        if ((bits & (1u << i)) && (mp.memoryTypes[i].propertyFlags & VK_MEMORY_PROPERTY_DEVICE_LOCAL_BIT)) return i;
    for (uint32_t i = 0; i < mp.memoryTypeCount; i++)
        if (bits & (1u << i)) return i;
    return UINT32_MAX;
}

// d3dx_device_ref returns a new reference to a D3D11VA hwdevice on the same
// GPU as the shared Vulkan device, with the shared fence imported into
// Vulkan -- or NULL if any piece is missing (no D3D11.4 fences, no external
// memory/semaphore support, no LUID match...), in which case the caller
// falls back to Vulkan Video decode.
AVBufferRef *d3dx_device_ref(void) {
    if (!g_dx_cs_init) { InitializeCriticalSection(&g_dx_cs); g_dx_cs_init = 1; }
    EnterCriticalSection(&g_dx_cs);
    if (g_dx_tried) {
        AVBufferRef *r = g_dx_hwdev ? av_buffer_ref(g_dx_hwdev) : NULL;
        LeaveCriticalSection(&g_dx_cs);
        return r;
    }
    g_dx_tried = 1;
    const char *fail = NULL;
    IDXGIFactory1 *fac = NULL;
    IDXGIAdapter1 *pick = NULL;
    ID3D11Device *dev0 = NULL;
    ID3D11DeviceContext *ctx0 = NULL;
    HANDLE fh = NULL;

    AVBufferRef *vkref = win_vk_hwdev_ctx_ref();
    if (!vkref) { fail = "no shared Vulkan device"; goto out; }
    AVVulkanDeviceContext *vk = (AVVulkanDeviceContext *)((AVHWDeviceContext *)vkref->data)->hwctx;
    g_dx_vkdev = vk->act_dev;
    g_dx_vkphys = vk->phys_dev;
    av_buffer_unref(&vkref); // the device itself lives for the process (vk_hwdev_bridge_windows.c)

    VkPhysicalDeviceIDProperties idp = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_ID_PROPERTIES };
    VkPhysicalDeviceProperties2 pp = { VK_STRUCTURE_TYPE_PHYSICAL_DEVICE_PROPERTIES_2 };
    pp.pNext = &idp;
    vkGetPhysicalDeviceProperties2(g_dx_vkphys, &pp);
    if (!idp.deviceLUIDValid) { fail = "Vulkan device has no LUID"; goto out; }

    if (CreateDXGIFactory1(&IID_IDXGIFactory1, (void **)&fac) != S_OK) { fail = "CreateDXGIFactory1"; goto out; }
    IDXGIAdapter1 *ad = NULL;
    for (UINT i = 0; IDXGIFactory1_EnumAdapters1(fac, i, &ad) == S_OK; i++) {
        DXGI_ADAPTER_DESC1 d;
        IDXGIAdapter1_GetDesc1(ad, &d);
        if (!memcmp(&d.AdapterLuid, idp.deviceLUID, sizeof(LUID))) { pick = ad; break; }
        IDXGIAdapter1_Release(ad);
    }
    if (!pick) { fail = "no DXGI adapter matches the Vulkan device"; goto out; }
    if (D3D11CreateDevice((IDXGIAdapter *)pick, D3D_DRIVER_TYPE_UNKNOWN, NULL, D3D11_CREATE_DEVICE_VIDEO_SUPPORT,
                          NULL, 0, D3D11_SDK_VERSION, &dev0, NULL, &ctx0) != S_OK) { fail = "D3D11CreateDevice"; goto out; }
    if (ID3D11Device_QueryInterface(dev0, &IID_ID3D11Device5, (void **)&g_dx_dev) != S_OK) { fail = "no ID3D11Device5 (fences)"; goto out; }
    if (ID3D11DeviceContext_QueryInterface(ctx0, &IID_ID3D11DeviceContext4, (void **)&g_dx_ctx) != S_OK) { fail = "no ID3D11DeviceContext4"; goto out; }

    if (ID3D11Device5_CreateFence(g_dx_dev, 0, D3D11_FENCE_FLAG_SHARED, &IID_ID3D11Fence, (void **)&g_dx_fence) != S_OK) { fail = "CreateFence"; goto out; }
    if (ID3D11Fence_CreateSharedHandle(g_dx_fence, NULL, GENERIC_ALL, NULL, &fh) != S_OK) { fail = "fence CreateSharedHandle"; goto out; }
    PFN_vkImportSemaphoreWin32HandleKHR importSem =
        (PFN_vkImportSemaphoreWin32HandleKHR)vkGetDeviceProcAddr(g_dx_vkdev, "vkImportSemaphoreWin32HandleKHR");
    if (!importSem || !vkGetDeviceProcAddr(g_dx_vkdev, "vkGetMemoryWin32HandlePropertiesKHR")) {
        fail = "Vulkan device lacks VK_KHR_external_{memory,semaphore}_win32"; goto out;
    }
    VkSemaphoreTypeCreateInfo stci = { VK_STRUCTURE_TYPE_SEMAPHORE_TYPE_CREATE_INFO };
    stci.semaphoreType = VK_SEMAPHORE_TYPE_TIMELINE;
    VkSemaphoreCreateInfo sci = { VK_STRUCTURE_TYPE_SEMAPHORE_CREATE_INFO };
    sci.pNext = &stci;
    if (vkCreateSemaphore(g_dx_vkdev, &sci, NULL, &g_dx_vksem) != VK_SUCCESS) { fail = "vkCreateSemaphore"; goto out; }
    VkImportSemaphoreWin32HandleInfoKHR isi = { VK_STRUCTURE_TYPE_IMPORT_SEMAPHORE_WIN32_HANDLE_INFO_KHR };
    isi.semaphore = g_dx_vksem;
    isi.handleType = VK_EXTERNAL_SEMAPHORE_HANDLE_TYPE_D3D11_FENCE_BIT;
    isi.handle = fh;
    if (importSem(g_dx_vkdev, &isi) != VK_SUCCESS) { fail = "import ID3D11Fence into Vulkan"; goto out; }

    AVBufferRef *hw = av_hwdevice_ctx_alloc(AV_HWDEVICE_TYPE_D3D11VA);
    if (!hw) { fail = "av_hwdevice_ctx_alloc(D3D11VA)"; goto out; }
    g_dx_avctx = (AVD3D11VADeviceContext *)((AVHWDeviceContext *)hw->data)->hwctx;
    g_dx_avctx->device = dev0; // ffmpeg owns this reference now
    dev0 = NULL;
    if (av_hwdevice_ctx_init(hw) < 0) { av_buffer_unref(&hw); g_dx_avctx = NULL; fail = "av_hwdevice_ctx_init(D3D11VA)"; goto out; }
    g_dx_hwdev = hw;

out:
    if (fh) CloseHandle(fh);
    if (ctx0) ID3D11DeviceContext_Release(ctx0);
    if (dev0) ID3D11Device_Release(dev0);
    if (pick) IDXGIAdapter1_Release(pick);
    if (fac) IDXGIFactory1_Release(fac);
    char msg[200];
    if (fail) {
        snprintf(msg, sizeof(msg), "libavcodec/win: D3D11VA->Vulkan interop unavailable: %s", fail);
    } else {
        snprintf(msg, sizeof(msg), "libavcodec/win: D3D11VA->Vulkan interop ready (shared fence imported)");
    }
    dx_log(msg);
    AVBufferRef *r = g_dx_hwdev ? av_buffer_ref(g_dx_hwdev) : NULL;
    LeaveCriticalSection(&g_dx_cs);
    return r;
}

static int dx_ring_busy(DxRing *r) {
    for (int i = 0; i < D3DX_RING; i++) if (r->slot[i].busy) return 1;
    return 0;
}

static void dx_ring_free(DxRing *r) {
    if (!r) return;
    for (int i = 0; i < D3DX_RING; i++) {
        DxSlot *s = &r->slot[i];
        if (s->img) { vk_video_forget_image((void *)s->img); vkDestroyImage(g_dx_vkdev, s->img, NULL); }
        if (s->mem) vkFreeMemory(g_dx_vkdev, s->mem, NULL);
        if (s->tex) ID3D11Texture2D_Release(s->tex);
    }
    free(r);
}

static DxRing *dx_ring_create(int w, int h, DXGI_FORMAT fmt, VkFormat vkfmt) {
    PFN_vkGetMemoryWin32HandlePropertiesKHR getProps =
        (PFN_vkGetMemoryWin32HandlePropertiesKHR)vkGetDeviceProcAddr(g_dx_vkdev, "vkGetMemoryWin32HandlePropertiesKHR");
    DxRing *r = (DxRing *)calloc(1, sizeof(DxRing));
    if (!r) return NULL;
    r->w = w; r->h = h; r->fmt = fmt;
    for (int i = 0; i < D3DX_RING; i++) {
        DxSlot *s = &r->slot[i];
        D3D11_TEXTURE2D_DESC td = { 0 };
        td.Width = w; td.Height = h; td.MipLevels = 1; td.ArraySize = 1; td.Format = fmt;
        td.SampleDesc.Count = 1; td.Usage = D3D11_USAGE_DEFAULT; td.BindFlags = D3D11_BIND_SHADER_RESOURCE;
        td.MiscFlags = D3D11_RESOURCE_MISC_SHARED | D3D11_RESOURCE_MISC_SHARED_NTHANDLE;
        if (ID3D11Device5_CreateTexture2D(g_dx_dev, &td, NULL, &s->tex) != S_OK) goto fail;
        IDXGIResource1 *res = NULL;
        HANDLE shared = NULL;
        ID3D11Texture2D_QueryInterface(s->tex, &IID_IDXGIResource1, (void **)&res);
        HRESULT hr = res ? IDXGIResource1_CreateSharedHandle(res, NULL, DXGI_SHARED_RESOURCE_READ | DXGI_SHARED_RESOURCE_WRITE, NULL, &shared) : E_FAIL;
        if (res) IDXGIResource1_Release(res);
        if (hr != S_OK) goto fail;

        VkExternalMemoryImageCreateInfo emi = { VK_STRUCTURE_TYPE_EXTERNAL_MEMORY_IMAGE_CREATE_INFO };
        emi.handleTypes = VK_EXTERNAL_MEMORY_HANDLE_TYPE_D3D11_TEXTURE_BIT;
        VkImageCreateInfo ici = { VK_STRUCTURE_TYPE_IMAGE_CREATE_INFO };
        ici.pNext = &emi; ici.imageType = VK_IMAGE_TYPE_2D; ici.format = vkfmt;
        ici.extent.width = w; ici.extent.height = h; ici.extent.depth = 1;
        ici.mipLevels = 1; ici.arrayLayers = 1; ici.samples = VK_SAMPLE_COUNT_1_BIT;
        ici.tiling = VK_IMAGE_TILING_OPTIMAL;
        ici.usage = VK_IMAGE_USAGE_SAMPLED_BIT | VK_IMAGE_USAGE_TRANSFER_SRC_BIT;
        ici.initialLayout = VK_IMAGE_LAYOUT_UNDEFINED;
        int ok = vkCreateImage(g_dx_vkdev, &ici, NULL, &s->img) == VK_SUCCESS;
        VkMemoryWin32HandlePropertiesKHR hp = { VK_STRUCTURE_TYPE_MEMORY_WIN32_HANDLE_PROPERTIES_KHR };
        ok = ok && getProps(g_dx_vkdev, VK_EXTERNAL_MEMORY_HANDLE_TYPE_D3D11_TEXTURE_BIT, shared, &hp) == VK_SUCCESS;
        if (ok) {
            VkMemoryRequirements mr;
            vkGetImageMemoryRequirements(g_dx_vkdev, s->img, &mr);
            VkMemoryDedicatedAllocateInfo ded = { VK_STRUCTURE_TYPE_MEMORY_DEDICATED_ALLOCATE_INFO };
            ded.image = s->img;
            VkImportMemoryWin32HandleInfoKHR imp = { VK_STRUCTURE_TYPE_IMPORT_MEMORY_WIN32_HANDLE_INFO_KHR };
            imp.pNext = &ded; imp.handleType = VK_EXTERNAL_MEMORY_HANDLE_TYPE_D3D11_TEXTURE_BIT; imp.handle = shared;
            VkMemoryAllocateInfo mai = { VK_STRUCTURE_TYPE_MEMORY_ALLOCATE_INFO };
            mai.pNext = &imp; mai.allocationSize = mr.size;
            mai.memoryTypeIndex = dx_mem_type(mr.memoryTypeBits & hp.memoryTypeBits);
            ok = vkAllocateMemory(g_dx_vkdev, &mai, NULL, &s->mem) == VK_SUCCESS &&
                 vkBindImageMemory(g_dx_vkdev, s->img, s->mem, 0) == VK_SUCCESS;
        }
        CloseHandle(shared);
        if (!ok) goto fail;
    }
    {
        char msg[160];
        snprintf(msg, sizeof(msg), "libavcodec/win: D3D11VA->Vulkan ring ready: %d x %dx%d %s",
                 D3DX_RING, w, h, fmt == DXGI_FORMAT_P010 ? "P010" : "NV12");
        dx_log(msg);
    }
    return r;
fail:
    dx_ring_free(r);
    return NULL;
}

void d3dx_release_slot(void *ctx) {
    DxSlot *s = (DxSlot *)ctx;
    if (s) InterlockedExchange(&s->busy, 0);
}

// d3dx_deliver copies one decoded D3D11VA frame into a free ring slot and
// signals the shared fence. On success it fills the VkImage/format/fence
// value the renderer should use and returns 1; the renderer gives the slot
// back through d3dx_release_slot(*out_release_ctx). Returns 0 (frame
// dropped, nothing to release) if no slot is free or the frame can't be
// mapped -- the renderer only ever shows the newest frame anyway.
int d3dx_deliver(AVFrame *frame, void **out_img, int *out_vkfmt, void **out_sem, uint64_t *out_val, void **out_release_ctx) {
    if (!g_dx_hwdev || frame->format != AV_PIX_FMT_D3D11 || !frame->hw_frames_ctx) return 0;
    AVHWFramesContext *fc = (AVHWFramesContext *)frame->hw_frames_ctx->data;
    DXGI_FORMAT fmt;
    VkFormat vkfmt;
    if (fc->sw_format == AV_PIX_FMT_NV12) {
        fmt = DXGI_FORMAT_NV12; vkfmt = VK_FORMAT_G8_B8R8_2PLANE_420_UNORM;
    } else if (fc->sw_format == AV_PIX_FMT_P010) {
        fmt = DXGI_FORMAT_P010; vkfmt = VK_FORMAT_G10X6_B10X6R10X6_2PLANE_420_UNORM_3PACK16;
    } else {
        return 0;
    }
    int w = frame->width, h = frame->height;
    if (g_dx_old && !dx_ring_busy(g_dx_old)) { dx_ring_free(g_dx_old); g_dx_old = NULL; }
    if (!g_dx_cur || w != g_dx_cur->w || h != g_dx_cur->h || fmt != g_dx_cur->fmt) {
        if (g_dx_old) return 0; // two size changes in flight; wait for the renderer to let go
        DxRing *r = dx_ring_create(w, h, fmt, vkfmt);
        if (!r) {
            static int logged = 0;
            if (!logged++) dx_log("libavcodec/win: D3D11VA->Vulkan ring creation failed -- frames dropped");
            return 0;
        }
        g_dx_old = g_dx_cur;
        g_dx_cur = r;
    }
    DxRing *ring = g_dx_cur;
    DxSlot *slot = NULL;
    for (int i = 0; i < D3DX_RING; i++) {
        DxSlot *s = &ring->slot[(ring->next + i) % D3DX_RING];
        if (InterlockedCompareExchange(&s->busy, 1, 0) == 0) {
            slot = s;
            ring->next = (int)((s - ring->slot) + 1) % D3DX_RING;
            break;
        }
    }
    if (!slot) return 0;

    ID3D11Texture2D *src = (ID3D11Texture2D *)frame->data[0];
    UINT index = (UINT)(intptr_t)frame->data[1];
    D3D11_BOX box = { 0, 0, 0, (UINT)w, (UINT)h, 1 };
    g_dx_avctx->lock(g_dx_avctx->lock_ctx);
    ID3D11DeviceContext4_CopySubresourceRegion(g_dx_ctx, (ID3D11Resource *)slot->tex, 0, 0, 0, 0,
                                               (ID3D11Resource *)src, index, &box);
    UINT64 v = ++g_dx_fence_val;
    ID3D11DeviceContext4_Signal(g_dx_ctx, g_dx_fence, v);
    ID3D11DeviceContext4_Flush(g_dx_ctx);
    g_dx_avctx->unlock(g_dx_avctx->lock_ctx);

    *out_img = (void *)slot->img;
    *out_vkfmt = (int)vkfmt;
    *out_sem = (void *)g_dx_vksem;
    *out_val = v;
    *out_release_ctx = slot;
    return 1;
}

#endif // _WIN32
