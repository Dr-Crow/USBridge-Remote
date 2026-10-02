#!/bin/bash
# Build the vendored PyroWave codec (third_party/pyrowave) as static archives for the
# Linux client's Vulkan decode path (internal/service/pyrowave_decode_linux.c).
#
# The tree is the same pin and local patches the hosts encode with (see
# third_party/pyrowave/vendor/pyrowave/PUNKTFUNK-VENDOR.txt): the bitstream has no version
# negotiation, so host and client must be built from the same source.
#
# Build deps: cmake, a C++17 compiler. Vulkan headers come from the vendored tree.
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC_DIR="${PROJECT_ROOT}/third_party/pyrowave"
BUILD_DIR="${SRC_DIR}/build"
NCPU="$(nproc 2>/dev/null || echo 2)"
JOBS="${PYROWAVE_BUILD_JOBS:-$NCPU}"

echo "==============================================="
echo " Building PyroWave (third_party/pyrowave)      "
echo "==============================================="

OUT_LIB="${BUILD_DIR}/libusbridge-pyrowave.a"
if [ -f "${OUT_LIB}" ] && [ -z "${PYROWAVE_FORCE_REBUILD:-}" ] && \
   [ -z "$(find "${SRC_DIR}/vendor" "${SRC_DIR}/CMakeLists.txt" "$0" -newer "${OUT_LIB}" -print -quit)" ]; then
    echo "✅ PyroWave already built."
    exit 0
fi

cmake -S "${SRC_DIR}" -B "${BUILD_DIR}" -DCMAKE_BUILD_TYPE=Release -DCMAKE_POSITION_INDEPENDENT_CODE=ON
cmake --build "${BUILD_DIR}" --target pyrowave-capi -j "${JOBS}"

# One relocatable object whose only global symbols are the pyrowave_* C API. Granite's
# volk defines every vk* entry point as a global function-pointer variable; linked as is,
# those would replace libvulkan's functions for the client's own Vulkan renderer
# (vk_video_impl_linux.c) and crash it. Localizing them keeps the two apart.
cd "${BUILD_DIR}"
ld -r -o pyrowave_all.o --whole-archive \
    libpyrowave-capi.a pyrowave/libpyrowave.a Granite/vulkan/libgranite-vulkan.a \
    Granite/util/libgranite-util.a Granite/math/libgranite-math.a Granite/third_party/libgranite-volk.a \
    --no-whole-archive
nm -g --defined-only pyrowave_all.o | awk '$3 ~ /^pyrowave_/ {print $3}' > pyrowave_api.txt
objcopy --keep-global-symbols=pyrowave_api.txt pyrowave_all.o
rm -f "${OUT_LIB}"
ar rcs "${OUT_LIB}" pyrowave_all.o
echo "✅ PyroWave built: ${OUT_LIB} ($(wc -l < pyrowave_api.txt) exported pyrowave_* symbols)"
