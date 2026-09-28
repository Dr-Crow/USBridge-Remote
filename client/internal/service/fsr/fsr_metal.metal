// fsr_metal.metal — AMD FidelityFX Super Resolution 1.0 (EASU + RCAS), hand
// ported to Metal Shading Language from the documented algorithm in
// ffx_fsr1.h (FsrEasuCon/FsrEasuF, FsrRcasCon/FsrRcasF) and ffx_a.h's
// bit-trick approximation helpers, in this same directory. NOT a mechanical
// translation of those AMD headers themselves (they only support
// HLSL/GLSL) -- this is a from-scratch MSL implementation of the same math,
// so ffx_a.h/ffx_fsr1.h stay untouched here.
//
// Compiled at RUNTIME via -[MTLDevice newLibraryWithSource:options:error:]
// (see metal_video_impl_darwin.m) rather than at build time -- this repo has
// no Metal Toolchain (`xcrun metal`/`metallib`) build step, and runtime
// compilation avoids adding one. Kept as a real .metal file (rather than an
// inline C string) purely for readability/diffability; it's embedded via
// go:embed, see metal_fsr_shader_darwin.go.
//
// Both kernels use a 1:1 threadgroup-per-output-pixel dispatch (no 8x8
// remapping/4-pixels-per-thread trick AMD's own sample shaders use for
// wave-occupancy reasons) -- simpler, and this app's frame sizes make that
// optimization not worth the extra complexity.

#include <metal_stdlib>
using namespace metal;

// ─── ffx_a.h's bit-trick approximation helpers ─────────────────────────────
inline float aPrxLoRcpF1(float a) {
    return as_type<float>(0x7ef07ebbu - as_type<uint>(a));
}
inline float aPrxMedRcpF1(float a) {
    float b = as_type<float>(0x7ef19fffu - as_type<uint>(a));
    return b * (-b * a + 2.0);
}
inline float aPrxLoRsqF1(float a) {
    return as_type<float>(0x5f347d74u - (as_type<uint>(a) >> 1u));
}
inline float aSatF1(float x) { return saturate(x); }

// ─── EASU: Edge Adaptive Spatial Upsampling ────────────────────────────────
// Const0..3: same packed-as-uint layout FsrEasuCon() produces in ffx_fsr1.h,
// computed CPU-side in metal_video_impl_darwin.m's fsr_easu_con().
struct FsrEasuConstants {
    uint4 const0;
    uint4 const1;
    uint4 const2;
    uint4 const3;
};

// Filtering for a given tap for the scalar.
inline void fsrEasuTapF(thread float3 &aC, thread float &aW,
                         float2 off, float2 dir, float2 len, float lob, float clp,
                         float3 c) {
    float2 v;
    v.x = (off.x * dir.x) + (off.y * dir.y);
    v.y = (off.x * -dir.y) + (off.y * dir.x);
    v *= len;
    float d2 = v.x * v.x + v.y * v.y;
    d2 = min(d2, clp);
    float wB = (2.0 / 5.0) * d2 - 1.0;
    float wA = lob * d2 - 1.0;
    wB *= wB;
    wA *= wA;
    wB = (25.0 / 16.0) * wB - (25.0 / 16.0 - 1.0);
    float w = wB * wA;
    aC += c * w;
    aW += w;
}

// Accumulate direction and length.
inline void fsrEasuSetF(thread float2 &dir, thread float &len, float2 pp,
                         bool biS, bool biT, bool biU, bool biV,
                         float lA, float lB, float lC, float lD, float lE) {
    float w = 0.0;
    if (biS) w = (1.0 - pp.x) * (1.0 - pp.y);
    if (biT) w =        pp.x  * (1.0 - pp.y);
    if (biU) w = (1.0 - pp.x) *        pp.y;
    if (biV) w =        pp.x  *        pp.y;

    float dc = lD - lC;
    float cb = lC - lB;
    float lenX = max(abs(dc), abs(cb));
    lenX = aPrxLoRcpF1(lenX);
    float dirX = lD - lB;
    dir.x += dirX * w;
    lenX = aSatF1(abs(dirX) * lenX);
    lenX *= lenX;
    len += lenX * w;

    float ec = lE - lC;
    float ca = lC - lA;
    float lenY = max(abs(ec), abs(ca));
    lenY = aPrxLoRcpF1(lenY);
    float dirY = lE - lA;
    dir.y += dirY * w;
    lenY = aSatF1(abs(dirY) * lenY);
    lenY *= lenY;
    len += lenY * w;
}

kernel void fsr_easu(texture2d<float, access::sample> inTex [[texture(0)]],
                      texture2d<float, access::write> outTex [[texture(1)]],
                      constant FsrEasuConstants &cb [[buffer(0)]],
                      uint2 ip [[thread_position_in_grid]]) {
    if (ip.x >= outTex.get_width() || ip.y >= outTex.get_height()) return;

    constexpr sampler s(coord::normalized, address::clamp_to_edge, filter::linear);
    float2 const0xy = as_type<float2>(cb.const0.xy);
    float2 const0zw = as_type<float2>(cb.const0.zw);
    float2 const1xy = as_type<float2>(cb.const1.xy);
    float2 const1zw = as_type<float2>(cb.const1.zw);
    float2 const2xy = as_type<float2>(cb.const2.xy);
    float2 const2zw = as_type<float2>(cb.const2.zw);
    float2 const3xy = as_type<float2>(cb.const3.xy);

    // Get position of 'f'.
    float2 pp = float2(ip) * const0xy + const0zw;
    float2 fp = floor(pp);
    pp -= fp;

    // 12-tap kernel -- gather4 at four 2x2 footprints (p0..p3), same
    // relative layout ffx_fsr1.h's own diagram documents. MSL's gather()
    // uses the same component ordering as desktop GLSL's textureGather
    // (x=(0,1) y=(1,1) z=(1,0) w=(0,0) relative to the sample point), which
    // is exactly what FsrEasuF's tap-naming below assumes.
    float2 p0 = fp * const1xy + const1zw;
    float2 p1 = p0 + const2xy;
    float2 p2 = p0 + const2zw;
    float2 p3 = p0 + const3xy;

    // NOTE: gather()'s 3rd positional parameter is an int2 texel OFFSET, not
    // the channel -- the channel selector is the 4th parameter, of type
    // `component` (an enum, not a plain int). Passing 0/1/2 positionally as
    // the 3rd argument silently compiles (as an offset of 0) but leaves the
    // channel at its default (component::x) for every call -- confirmed
    // live: a synthetic 6-color-band test frame came back fully grayscale
    // (every output channel equal to the source's R channel) until this was
    // fixed to pass an explicit offset + component.
    float4 bczzR = inTex.gather(s, p0, int2(0), component::x);
    float4 bczzG = inTex.gather(s, p0, int2(0), component::y);
    float4 bczzB = inTex.gather(s, p0, int2(0), component::z);
    float4 ijfeR = inTex.gather(s, p1, int2(0), component::x);
    float4 ijfeG = inTex.gather(s, p1, int2(0), component::y);
    float4 ijfeB = inTex.gather(s, p1, int2(0), component::z);
    float4 klhgR = inTex.gather(s, p2, int2(0), component::x);
    float4 klhgG = inTex.gather(s, p2, int2(0), component::y);
    float4 klhgB = inTex.gather(s, p2, int2(0), component::z);
    float4 zzonR = inTex.gather(s, p3, int2(0), component::x);
    float4 zzonG = inTex.gather(s, p3, int2(0), component::y);
    float4 zzonB = inTex.gather(s, p3, int2(0), component::z);

    // Luma times 2 (2 FMAs).
    float4 bczzL = bczzB * 0.5 + (bczzR * 0.5 + bczzG);
    float4 ijfeL = ijfeB * 0.5 + (ijfeR * 0.5 + ijfeG);
    float4 klhgL = klhgB * 0.5 + (klhgR * 0.5 + klhgG);
    float4 zzonL = zzonB * 0.5 + (zzonR * 0.5 + zzonG);

    float bL = bczzL.x, cL = bczzL.y;
    float iL = ijfeL.x, jL = ijfeL.y, fL = ijfeL.z, eL = ijfeL.w;
    float kL = klhgL.x, lL = klhgL.y, hL = klhgL.z, gL = klhgL.w;
    float oL = zzonL.z, nL = zzonL.w;

    float2 dir = float2(0.0);
    float len = 0.0;
    fsrEasuSetF(dir, len, pp, true,  false, false, false, bL, eL, fL, gL, jL);
    fsrEasuSetF(dir, len, pp, false, true,  false, false, cL, fL, gL, hL, kL);
    fsrEasuSetF(dir, len, pp, false, false, true,  false, fL, iL, jL, kL, nL);
    fsrEasuSetF(dir, len, pp, false, false, false, true,  gL, jL, kL, lL, oL);

    float2 dir2 = dir * dir;
    float dirR = dir2.x + dir2.y;
    bool zro = dirR < (1.0 / 32768.0);
    dirR = aPrxLoRsqF1(dirR);
    dirR = zro ? 1.0 : dirR;
    dir.x = zro ? 1.0 : dir.x;
    dir *= dirR;

    len = len * 0.5;
    len *= len;
    float stretch = (dir.x * dir.x + dir.y * dir.y) * aPrxLoRcpF1(max(abs(dir.x), abs(dir.y)));
    float2 len2 = float2(1.0 + (stretch - 1.0) * len, 1.0 - 0.5 * len);
    float lob = 0.5 + ((1.0 / 4.0 - 0.04) - 0.5) * len;
    float clp = aPrxLoRcpF1(lob);

    float3 min4 = min(min(float3(ijfeR.z, ijfeG.z, ijfeB.z), min(float3(klhgR.w, klhgG.w, klhgB.w), float3(ijfeR.y, ijfeG.y, ijfeB.y))),
                       float3(klhgR.x, klhgG.x, klhgB.x));
    float3 max4 = max(max(float3(ijfeR.z, ijfeG.z, ijfeB.z), max(float3(klhgR.w, klhgG.w, klhgB.w), float3(ijfeR.y, ijfeG.y, ijfeB.y))),
                       float3(klhgR.x, klhgG.x, klhgB.x));

    float3 aC = float3(0.0);
    float aW = 0.0;
    fsrEasuTapF(aC, aW, float2( 0.0,-1.0) - pp, dir, len2, lob, clp, float3(bczzR.x, bczzG.x, bczzB.x)); // b
    fsrEasuTapF(aC, aW, float2( 1.0,-1.0) - pp, dir, len2, lob, clp, float3(bczzR.y, bczzG.y, bczzB.y)); // c
    fsrEasuTapF(aC, aW, float2(-1.0, 1.0) - pp, dir, len2, lob, clp, float3(ijfeR.x, ijfeG.x, ijfeB.x)); // i
    fsrEasuTapF(aC, aW, float2( 0.0, 1.0) - pp, dir, len2, lob, clp, float3(ijfeR.y, ijfeG.y, ijfeB.y)); // j
    fsrEasuTapF(aC, aW, float2( 0.0, 0.0) - pp, dir, len2, lob, clp, float3(ijfeR.z, ijfeG.z, ijfeB.z)); // f
    fsrEasuTapF(aC, aW, float2(-1.0, 0.0) - pp, dir, len2, lob, clp, float3(ijfeR.w, ijfeG.w, ijfeB.w)); // e
    fsrEasuTapF(aC, aW, float2( 1.0, 1.0) - pp, dir, len2, lob, clp, float3(klhgR.x, klhgG.x, klhgB.x)); // k
    fsrEasuTapF(aC, aW, float2( 2.0, 1.0) - pp, dir, len2, lob, clp, float3(klhgR.y, klhgG.y, klhgB.y)); // l
    fsrEasuTapF(aC, aW, float2( 2.0, 0.0) - pp, dir, len2, lob, clp, float3(klhgR.z, klhgG.z, klhgB.z)); // h
    fsrEasuTapF(aC, aW, float2( 1.0, 0.0) - pp, dir, len2, lob, clp, float3(klhgR.w, klhgG.w, klhgB.w)); // g
    fsrEasuTapF(aC, aW, float2( 1.0, 2.0) - pp, dir, len2, lob, clp, float3(zzonR.z, zzonG.z, zzonB.z)); // o
    fsrEasuTapF(aC, aW, float2( 0.0, 2.0) - pp, dir, len2, lob, clp, float3(zzonR.w, zzonG.w, zzonB.w)); // n

    float3 pix = min(max4, max(min4, aC * aPrxMedRcpF1(aW)));
    outTex.write(float4(pix, 1.0), ip);
}

// ─── RCAS: Robust Contrast Adaptive Sharpening ─────────────────────────────
struct FsrRcasConstants {
    uint4 const0; // .x = bitcast(sharpness); rest unused (see fsr_rcas_con())
};

kernel void fsr_rcas(texture2d<float, access::read> inTex [[texture(0)]],
                      texture2d<float, access::write> outTex [[texture(1)]],
                      constant FsrRcasConstants &cb [[buffer(0)]],
                      uint2 ip [[thread_position_in_grid]]) {
    if (ip.x >= outTex.get_width() || ip.y >= outTex.get_height()) return;

    int w = int(inTex.get_width()), h = int(inTex.get_height());
    int2 sp = int2(ip);
    auto clampRead = [&](int2 p) {
        p = clamp(p, int2(0), int2(w - 1, h - 1));
        return inTex.read(uint2(p));
    };

    float3 b = clampRead(sp + int2( 0,-1)).rgb;
    float3 d = clampRead(sp + int2(-1, 0)).rgb;
    float3 e = clampRead(sp).rgb;
    float3 f = clampRead(sp + int2( 1, 0)).rgb;
    float3 h_ = clampRead(sp + int2( 0, 1)).rgb;

    float bR=b.r, bG=b.g, bB=b.b;
    float dR=d.r, dG=d.g, dB=d.b;
    float eR=e.r, eG=e.g, eB=e.b;
    float fR=f.r, fG=f.g, fB=f.b;
    float hR=h_.r, hG=h_.g, hB=h_.b;

    float bL = bB*0.5 + (bR*0.5 + bG);
    float dL = dB*0.5 + (dR*0.5 + dG);
    float eL = eB*0.5 + (eR*0.5 + eG);
    float fL = fB*0.5 + (fR*0.5 + fG);
    float hL = hB*0.5 + (hR*0.5 + hG);

    float nz = 0.25*bL + 0.25*dL + 0.25*fL + 0.25*hL - eL;
    nz = aSatF1(abs(nz) * aPrxMedRcpF1(max(max(bL,dL),max(eL,max(fL,hL))) - min(min(bL,dL),min(eL,min(fL,hL)))));
    nz = -0.5*nz + 1.0;

    float mn4R = min(min(bR,dR),min(fR,hR));
    float mn4G = min(min(bG,dG),min(fG,hG));
    float mn4B = min(min(bB,dB),min(fB,hB));
    float mx4R = max(max(bR,dR),max(fR,hR));
    float mx4G = max(max(bG,dG),max(fG,hG));
    float mx4B = max(max(bB,dB),max(fB,hB));

    float peakCx = 1.0, peakCy = -4.0;
    float hitMinR = min(mn4R,eR) * aPrxMedRcpF1(4.0*mx4R);
    float hitMinG = min(mn4G,eG) * aPrxMedRcpF1(4.0*mx4G);
    float hitMinB = min(mn4B,eB) * aPrxMedRcpF1(4.0*mx4B);
    float hitMaxR = (peakCx - max(mx4R,eR)) * aPrxMedRcpF1(4.0*mn4R + peakCy);
    float hitMaxG = (peakCx - max(mx4G,eG)) * aPrxMedRcpF1(4.0*mn4G + peakCy);
    float hitMaxB = (peakCx - max(mx4B,eB)) * aPrxMedRcpF1(4.0*mn4B + peakCy);
    float lobeR = max(-hitMinR, hitMaxR);
    float lobeG = max(-hitMinG, hitMaxG);
    float lobeB = max(-hitMinB, hitMaxB);

    const float kRcasLimit = 0.25 - (1.0 / 16.0);
    float sharpness = as_type<float>(cb.const0.x);
    float lobe = max(-kRcasLimit, min(max(max(lobeR,lobeG),lobeB), 0.0)) * sharpness;

    float rcpL = aPrxMedRcpF1(4.0*lobe + 1.0);
    float3 pix;
    pix.r = (lobe*bR + lobe*dR + lobe*hR + lobe*fR + eR) * rcpL;
    pix.g = (lobe*bG + lobe*dG + lobe*hG + lobe*fG + eG) * rcpL;
    pix.b = (lobe*bB + lobe*dB + lobe*hB + lobe*fB + eB) * rcpL;

    outTex.write(float4(pix, 1.0), ip);
}

// ─── Plain bilinear resample (also used as the "no upscale ratio" / non-FSR
// fallback path, see metal_video_impl_darwin.m) ────────────────────────────
kernel void resample_bilinear(texture2d<float, access::sample> inTex [[texture(0)]],
                               texture2d<float, access::write> outTex [[texture(1)]],
                               uint2 gid [[thread_position_in_grid]]) {
    if (gid.x >= outTex.get_width() || gid.y >= outTex.get_height()) return;
    constexpr sampler s(coord::normalized, address::clamp_to_edge, filter::linear);
    float2 uv = (float2(gid) + 0.5) / float2(outTex.get_width(), outTex.get_height());
    outTex.write(inTex.sample(s, uv), gid);
}

// ─── Bicubic (Catmull-Rom, B=0 C=0.5) and Lanczos(a=2) resample kernels ────
// Both are plain 4x4-tap weighted resamples -- the "quality" alternatives to
// resample_bilinear above for the base video-to-window stretch (independent
// of FSR1's EASU+RCAS, which is a different, sharpening-aware algorithm for
// the specific case of upscaling a lower-than-native stream resolution).
inline float resampleCubicWeight(float x) {
    x = abs(x);
    if (x < 1.0) return 1.5*x*x*x - 2.5*x*x + 1.0;
    if (x < 2.0) return -0.5*x*x*x + 2.5*x*x - 4.0*x + 2.0;
    return 0.0;
}

inline float resampleSinc(float x) {
    if (abs(x) < 1e-5) return 1.0;
    float px = M_PI_F * x;
    return sin(px) / px;
}

inline float resampleLanczosWeight(float x, float a) {
    x = abs(x);
    if (x >= a) return 0.0;
    return resampleSinc(x) * resampleSinc(x / a);
}

// Shared 4x4-neighborhood weighted resample -- `weightFn` supplies either
// the cubic or Lanczos 1D kernel, separably applied per axis.
template <typename WeightFn>
inline float4 resample4x4(texture2d<float, access::read> inTex, uint2 gid, texture2d<float, access::write> outTex, WeightFn weightFn) {
    int srcW = int(inTex.get_width()), srcH = int(inTex.get_height());
    float2 scale = float2(srcW, srcH) / float2(outTex.get_width(), outTex.get_height());
    float2 srcPos = (float2(gid) + 0.5) * scale - 0.5;
    float2 srcFloor = floor(srcPos);
    float2 frac = srcPos - srcFloor;

    float4 sum = float4(0.0);
    float wsum = 0.0;
    for (int dy = -1; dy <= 2; dy++) {
        float wy = weightFn(float(dy) - frac.y);
        int2 p; p.y = clamp(int(srcFloor.y) + dy, 0, srcH - 1);
        for (int dx = -1; dx <= 2; dx++) {
            float w = weightFn(float(dx) - frac.x) * wy;
            p.x = clamp(int(srcFloor.x) + dx, 0, srcW - 1);
            sum += inTex.read(uint2(p)) * w;
            wsum += w;
        }
    }
    return wsum > 0.0 ? sum / wsum : sum;
}

kernel void resample_bicubic(texture2d<float, access::read> inTex [[texture(0)]],
                              texture2d<float, access::write> outTex [[texture(1)]],
                              uint2 gid [[thread_position_in_grid]]) {
    if (gid.x >= outTex.get_width() || gid.y >= outTex.get_height()) return;
    outTex.write(resample4x4(inTex, gid, outTex, resampleCubicWeight), gid);
}

kernel void resample_lanczos(texture2d<float, access::read> inTex [[texture(0)]],
                              texture2d<float, access::write> outTex [[texture(1)]],
                              uint2 gid [[thread_position_in_grid]]) {
    if (gid.x >= outTex.get_width() || gid.y >= outTex.get_height()) return;
    outTex.write(resample4x4(inTex, gid, outTex, [](float x){ return resampleLanczosWeight(x, 2.0); }), gid);
}

// ─── HDR path: BT.2020 limited-range 10-bit biplanar YCbCr -> RGB ─────────
// Converts kCVPixelFormatType_420YpCbCr10BiPlanarVideoRange's two planes
// (luma R16Unorm, chroma RG16Unorm -- see metal_video_impl_darwin.m's
// import code) into an RGB texture that EASU/RCAS/bilinear/bicubic/Lanczos
// above can then upscale exactly like the SDR BGRA path, unchanged.
//
// Deliberately produces PQ-ENCODED (non-linear) BT.2020 RGB, NOT
// display-linear light: this mirrors exactly what Core Animation's own
// compositor does today when handed the same tagged IOSurface directly (see
// metal_video_set_hdr's doc comment in metal_video_impl_darwin.m) -- the
// actual PQ EOTF + display tone-mapping stays the OS's job, driven by
// tagging g_metal_layer.colorspace as kCGColorSpaceITUR_2100_PQ (see
// metal_spike_render), not something this kernel computes itself. That's
// deliberate: getting the EOTF/tone-mapping right for a specific display is
// exactly the part Apple's own color pipeline is already trusted for one
// line up (the existing g_layer path); duplicating it here would only add
// risk, not correctness.
//
// R16Unorm/RG16Unorm sampling normalizes the full 16-bit value to [0,1];
// CoreVideo's 10-bit-in-16-bit convention left-shifts the 10-bit sample by
// 6 bits, so the code value is recovered as sampled*65535/64 (not /1023 --
// division by the WRONG constant here would silently crush contrast across
// the whole image, exactly the kind of "subtly wrong, not obviously broken"
// error this file's own doc comment warns about).
inline float3 yuv2020VideoRangeToRGB(float yNorm, float cbNorm, float crNorm) {
    const float codeMax = 65535.0 / 64.0; // recovers the 10-bit code value from a 16-bit normalized sample
    float yCode  = yNorm  * codeMax;
    float cbCode = cbNorm * codeMax;
    float crCode = crNorm * codeMax;

    // Limited (video) range, 10-bit: luma [64,940], chroma [64,960] centered on 512.
    float yp = (yCode  -  64.0) / 876.0;
    float cb = (cbCode - 512.0) / 896.0;
    float cr = (crCode - 512.0) / 896.0;

    // BT.2020 non-constant-luminance YCbCr->RGB matrix (Kr=0.2627, Kb=0.0593).
    float r = yp + 1.4746 * cr;
    float g = yp - 0.16455 * cb - 0.57135 * cr;
    float b = yp + 1.8814 * cb;
    return saturate(float3(r, g, b));
}

kernel void yuv2020_to_rgb(texture2d<float, access::sample> yTex  [[texture(0)]],
                            texture2d<float, access::sample> cbcrTex [[texture(1)]],
                            texture2d<float, access::write>  outTex  [[texture(2)]],
                            uint2 gid [[thread_position_in_grid]]) {
    if (gid.x >= outTex.get_width() || gid.y >= outTex.get_height()) return;
    constexpr sampler s(coord::normalized, address::clamp_to_edge, filter::linear);
    float2 uv = (float2(gid) + 0.5) / float2(outTex.get_width(), outTex.get_height());
    float y = yTex.sample(s, uv).r;
    // Chroma plane is half resolution (4:2:0) -- same uv works since it's
    // sampled from its own, separately-sized texture with a linear filter
    // (bilinear chroma upsampling, standard practice for 4:2:0 display).
    float2 cbcr = cbcrTex.sample(s, uv).rg;
    float3 rgb = yuv2020VideoRangeToRGB(y, cbcr.r, cbcr.g);
    outTex.write(float4(rgb, 1.0), gid);
}
