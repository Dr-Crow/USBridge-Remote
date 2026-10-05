#!/bin/bash
# Build the vendored PyroWave codec (third_party/pyrowave) as one static archive for the
# Windows client's GPU decode path (internal/service/pyrowave_decode_windows.c).
# MSYS2 UCRT64: cmake, ninja and the UCRT64 gcc/binutils.
#
# Same source pin as the hosts and the Linux/macOS clients (see
# third_party/pyrowave/vendor/pyrowave/PUNKTFUNK-VENDOR.txt): the bitstream has no version
# negotiation, so host and client must be built from the same tree.
#
# Unlike build_pyrowave.sh (ELF), the archives can't be reduced to "only pyrowave_* stay
# global" here: on COFF, Granite's template instantiations live in COMDAT sections that are
# resolved by global name, and localizing them breaks the link. What actually matters is the
# same as on Linux: Granite's volk defines every vk* entry point as a global function-pointer
# variable, which would replace vulkan-1's functions for the client's own Vulkan renderer
# (vk_video_impl_windows.c). Those -- and only those -- are localized.
set -euo pipefail

PROJECT_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
SRC_DIR="${PROJECT_ROOT}/third_party/pyrowave"
BUILD_DIR="${SRC_DIR}/build-win"
JOBS="${PYROWAVE_BUILD_JOBS:-$(nproc 2>/dev/null || echo 2)}"

echo "==============================================="
echo " Building PyroWave for Windows (third_party/pyrowave)"
echo "==============================================="

OUT_LIB="${BUILD_DIR}/libusbridge-pyrowave.a"
if [ -f "${OUT_LIB}" ] && [ -z "${PYROWAVE_FORCE_REBUILD:-}" ] && \
   [ -z "$(find "${SRC_DIR}/vendor" "${SRC_DIR}/CMakeLists.txt" "$0" -newer "${OUT_LIB}" -print -quit)" ]; then
    echo "✅ PyroWave already built."
    exit 0
fi

GENERATOR=()
command -v ninja >/dev/null 2>&1 && GENERATOR=(-G Ninja)
cmake -S "${SRC_DIR}" -B "${BUILD_DIR}" "${GENERATOR[@]}" -DCMAKE_BUILD_TYPE=Release \
    -DCMAKE_C_COMPILER=gcc -DCMAKE_CXX_COMPILER=g++ \
    -DCMAKE_CXX_STANDARD=17 -DCMAKE_CXX_STANDARD_REQUIRED=ON
cmake --build "${BUILD_DIR}" --target pyrowave-capi -j "${JOBS}"

cd "${BUILD_DIR}"
ld -r -o pyrowave_all.o --whole-archive \
    libpyrowave-capi.a pyrowave/libpyrowave.a Granite/vulkan/libgranite-vulkan.a \
    Granite/util/libgranite-util.a Granite/math/libgranite-math.a Granite/third_party/libgranite-volk.a \
    --no-whole-archive
nm -g --defined-only pyrowave_all.o | awk '$3 ~ /^vk[A-Z]/ || $3 ~ /^volk/ {print $3}' > volk_symbols.txt
objcopy --localize-symbols=volk_symbols.txt pyrowave_all.o
if nm -g --defined-only pyrowave_all.o | awk '$3 ~ /^vk[A-Z]/' | grep -q .; then
    echo "❌ vk* symbols still global in pyrowave_all.o -- they would shadow vulkan-1.dll" >&2
    exit 1
fi
rm -f "${OUT_LIB}"
ar rcs "${OUT_LIB}" pyrowave_all.o
echo "✅ PyroWave built: ${OUT_LIB} ($(wc -l < volk_symbols.txt) volk symbols localized)"
