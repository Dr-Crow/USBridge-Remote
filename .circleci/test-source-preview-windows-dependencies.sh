#!/usr/bin/env bash
# Resolve only public client/native toolchains before its isolated cache lookup.
set -euo pipefail
if [[ -n "${USBRIDGE_CI_ROOT:-}" ]]; then cd "$(cygpath -u "$USBRIDGE_CI_ROOT")"; fi
ROOT="$PWD"; WORK="$ROOT/.source-preview-windows-build"
export PATH="/c/ci/go/bin:/ucrt64/bin:/usr/bin:$PATH" GOROOT='C:\ci\go' GOTOOLCHAIN=local
[[ "$(go version)" == 'go version go1.26.9 windows/amd64' ]]
mkdir -p "$WORK"
pacman --noconfirm -S --needed mingw-w64-ucrt-x86_64-cmake mingw-w64-ucrt-x86_64-ninja \
 mingw-w64-ucrt-x86_64-pkgconf mingw-w64-ucrt-x86_64-opus mingw-w64-ucrt-x86_64-openssl \
 mingw-w64-ucrt-x86_64-ffmpeg mingw-w64-ucrt-x86_64-vulkan-headers mingw-w64-ucrt-x86_64-vulkan-loader
pacman -Q > "$WORK/packages.txt"
PYTHONPATH=.circleci/source python3 -m unittest discover -s .circleci/source -p 'test_windows_viewer_cache_key.py'
python3 .circleci/source/windows_viewer_cache_key.py --root "$(cygpath -m "$ROOT")" --work "$(cygpath -m "$WORK")"
