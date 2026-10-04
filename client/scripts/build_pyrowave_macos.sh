#!/bin/bash
# Build the vendored PyroWave codec (third_party/pyrowave) as a static archive for the
# macOS client's decode path (internal/service/pyrowave_decode_darwin.m).
#
# macOS has no native Vulkan: PyroWave's compute decoder runs through MoltenVK (Vulkan-
# over-Metal translation) instead. volk (PyroWave/Granite's Vulkan function loader) already
# has a macOS dlopen fallback chain that tries "libMoltenVK.dylib" directly with no Vulkan
# Loader in between, so nothing in this build links against libvulkan/libMoltenVK at all --
# that dependency is resolved at RUNTIME (see pyrowave_decode_darwin.m's preload and
# build_macos.sh's Frameworks/libMoltenVK.dylib bundling).
#
# The tree is the same pin and local patches the hosts encode with (see
# third_party/pyrowave/vendor/pyrowave/PUNKTFUNK-VENDOR.txt): the bitstream has no version
# negotiation, so host and client must be built from the same source. One portability fix on
# top for this platform: Granite/util/timer.cpp used clock_nanosleep(TIMER_ABSTIME), which
# Darwin doesn't implement (see that file's own __APPLE__ branch).
#
# PyroWave on macOS only needs to DECODE (rust-shine hosts encode on their own GPU, over on
# Linux/Windows); its own device-side ENCODE requires a Vulkan feature (requiredSubgroupSize
# pipeline stages, i.e. VK_EXT_subgroup_size_control's fixed-size mode) that MoltenVK does not
# implement on Apple Silicon -- harmless here since pyrowave_decoder_create's own device gate
# is the more lenient "varying subgroup size" check, which MoltenVK does satisfy (validated
# directly against a real Apple M1 via MoltenVK 1.4.2: pyrowave_decoder_create succeeds).
#
# Build deps: cmake, a C++17 compiler. Vulkan headers come from the vendored tree (same as
# Linux) -- MoltenVK/vulkan-loader are a runtime-only dependency, not needed to build this.
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC_DIR="${PROJECT_ROOT}/third_party/pyrowave"
BUILD_DIR="${SRC_DIR}/build"
NCPU="$(sysctl -n hw.ncpu 2>/dev/null || echo 2)"
JOBS="${PYROWAVE_BUILD_JOBS:-$NCPU}"

echo "==============================================="
echo " Building PyroWave (third_party/pyrowave) macOS "
echo "==============================================="

OUT_LIB="${BUILD_DIR}/libusbridge-pyrowave.a"
if [ -f "${OUT_LIB}" ] && [ -z "${PYROWAVE_FORCE_REBUILD:-}" ] && \
   [ -z "$(find "${SRC_DIR}/vendor" "${SRC_DIR}/CMakeLists.txt" "$0" -newer "${OUT_LIB}" -print -quit)" ]; then
    echo "✅ PyroWave already built."
    exit 0
fi

# -DCMAKE_CXX_STANDARD/REQUIRED forced on the command line (a cache entry,
# outranks the vendor's own `set(CMAKE_CXX_STANDARD 14)` before its
# project() call): that plain set() stopped taking effect on this runner
# image (macos-14) as of 2026-10, silently falling back to a pre-C++11
# dialect and failing on Granite's `constexpr`/alias-declaration/rvalue-ref
# usage ("too many errors emitted" cascading from intrusive.hpp/
# logging.hpp). Setting it explicitly here is immune to whatever changed on
# the image side.
cmake -S "${SRC_DIR}" -B "${BUILD_DIR}" -DCMAKE_BUILD_TYPE=Release -DCMAKE_POSITION_INDEPENDENT_CODE=ON \
    -DCMAKE_CXX_STANDARD=17 -DCMAKE_CXX_STANDARD_REQUIRED=ON
cmake --build "${BUILD_DIR}" --target pyrowave-capi -j "${JOBS}"

# One combined archive so the Go side only needs one -l flag (matches Linux's
# libusbridge-pyrowave.a convention, see build_pyrowave.sh). Unlike Linux, nothing here needs
# symbol hiding: that step exists there solely to stop volk's global vk* function-pointer
# variables from clobbering the client's OWN Vulkan renderer -- this client has no Vulkan
# renderer at all (video goes through Metal/VideoToolbox), so there's nothing to collide with.
cd "${BUILD_DIR}"
libtool -static -o pyrowave_all.a \
    libpyrowave-capi.a pyrowave/libpyrowave.a Granite/vulkan/libgranite-vulkan.a \
    Granite/util/libgranite-util.a Granite/math/libgranite-math.a Granite/third_party/libgranite-volk.a
rm -f "${OUT_LIB}"
mv pyrowave_all.a "${OUT_LIB}"
echo "✅ PyroWave built: ${OUT_LIB}"
