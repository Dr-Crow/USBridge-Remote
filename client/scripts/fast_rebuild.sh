#!/bin/bash
# Fast incremental rebuild — Go only, no DLL copy, no fyne package.
# The exe goes in dist/windows/bin/ next to DLLs from a previous
# build_windows.sh (avcodec, avutil, libcrypto, libopus, …). Copying to
# dist/windows/ itself launches without those DLLs.
# Use from UCRT64 shell: ./scripts/fast_rebuild.sh
# Run time: ~15-30s on cached build (only changed packages recompile).
#
# For a full dist rebuild with all DLLs: ./scripts/build_windows.sh
#
# Keep CGO flags / toolchain aligned with build_windows.sh so this hits the
# same GOCACHE. Do not inject a unique CGO_CFLAGS=-I/ucrt64/include: that
# busts the cache (full Moonlight cgo rebuild, looks "stuck") and pulls
# windows.h before winsock2.h.
#
# -tags usbpass_gousb must match build_windows.sh, or this overwrites the
# real libusb/WinUSB claim path with backend_nogousb.go's disabled stub --
# USB passthrough then silently exports every device as a fake MSC
# descriptor instead of claiming it (see docs/USB_PASSTHROUGH.md).
set -e

REPO_ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
EXE_OUT="$REPO_ROOT/.cache/build/windows-amd64/release/USBridge_Client.exe"
DIST_DIR="$REPO_ROOT/dist/windows/bin"
DIST_EXE="$DIST_DIR/USBridge_Client.exe"

# UCRT64 gcc/pkg-config ahead of Strawberry Perl MinGW (empty .idata / wrong
# pkg-config database). Same idea as fast_rebuild.ps1.
if [ -d /ucrt64/bin ]; then
  export PATH="/ucrt64/bin:/usr/bin:${PATH}"
fi
if [ -x /ucrt64/bin/gcc ]; then
  export CC="${CC:-/ucrt64/bin/gcc}"
  export CXX="${CXX:-/ucrt64/bin/g++}"
fi
if [ -x /ucrt64/bin/pkg-config ]; then
  export PKG_CONFIG="${PKG_CONFIG:-/ucrt64/bin/pkg-config}"
fi

# Drop unpacked moonlight openssl/opus (and android opus.pc) from pkg-config.
# Those trees are local build artifacts, not the Windows host libs the cgo
# #cgo pkg-config: opus openssl line should use.
filter_pc_path() {
  local in="${1:-}"
  local sep="$2"
  local out="" p
  [ -z "$in" ] && return 0
  IFS="$sep"
  # shellcheck disable=SC2086
  for p in $in; do
    case "$p" in
      *moonlight-common-c/openssl-3.3.2*|\
      *moonlight-common-c/opus-1.5.2*|\
      *moonlight-common-c/opus-cmake-build*|\
      *moonlight-common-c/build/android*)
        continue
        ;;
    esac
    [ -z "$p" ] && continue
    if [ -z "$out" ]; then
      out="$p"
    else
      out="${out}${sep}${p}"
    fi
  done
  printf '%s' "$out"
}

if [ -n "${PKG_CONFIG_PATH:-}" ]; then
  if [[ "${PKG_CONFIG_PATH}" == *";"* ]]; then
    PKG_CONFIG_PATH="$(filter_pc_path "$PKG_CONFIG_PATH" ';')"
  else
    PKG_CONFIG_PATH="$(filter_pc_path "$PKG_CONFIG_PATH" ':')"
  fi
  export PKG_CONFIG_PATH
fi
if [ -d /ucrt64/lib/pkgconfig ]; then
  case ":${PKG_CONFIG_PATH:-}:" in
    *:/ucrt64/lib/pkgconfig:*) ;;
    *) export PKG_CONFIG_PATH="/ucrt64/lib/pkgconfig${PKG_CONFIG_PATH:+:$PKG_CONFIG_PATH}" ;;
  esac
fi

echo "==> fast_rebuild: go build..."
echo "==> CC=${CC:-$(command -v gcc 2>/dev/null || echo unset)} PKG_CONFIG=${PKG_CONFIG:-$(command -v pkg-config 2>/dev/null || echo unset)}"
cd "$REPO_ROOT/cmd"
export CGO_ENABLED=1
export GOOS=windows
export GOARCH=amd64
export GOCACHE="${GOCACHE:-$REPO_ROOT/.cache/go-build/windows-amd64}"
export GOMODCACHE="${GOMODCACHE:-$REPO_ROOT/.cache/go-mod}"
export GOMAXPROCS="${GOMAXPROCS:-12}"
export GOFLAGS="${GOFLAGS:-} -buildvcs=false"
mkdir -p "$GOCACHE" "$GOMODCACHE" "$(dirname "$EXE_OUT")" "$(dirname "$DIST_EXE")"
cp "$REPO_ROOT/VERSION" "$REPO_ROOT/cmd/VERSION" 2>/dev/null || true
VERSION=$(cat "$REPO_ROOT/VERSION" 2>/dev/null || echo "1.0.0")

# Intentionally do not export CGO_CFLAGS / CGO_LDFLAGS here. gcc already
# searches /ucrt64/include and /ucrt64/lib; extra -I/-L only invalidates
# GOCACHE versus scripts/build_windows.sh.

echo "==> Compiling. The winsock2.h warning is normal."
echo "==> gcc can stay silent for 1–5 min after it (Moonlight cgo) — wait for 'Copying to dist'."

# Heartbeat so a quiet gcc compile is not mistaken for a hang.
heartbeat() {
  local n=0
  while sleep 20; do
    n=$((n + 20))
    echo "==> still compiling (${n}s)..."
  done
}
heartbeat &
hb=$!
trap 'kill "$hb" 2>/dev/null || true' EXIT

set +e
time go build \
  -trimpath \
  -v \
  -tags usbpass_gousb \
  -ldflags="-H=windowsgui -X main.version=$VERSION -extldflags=-Wl,--stack,8388608" \
  -o "$EXE_OUT" \
  .
st=$?
set -e
kill "$hb" 2>/dev/null || true
wait "$hb" 2>/dev/null || true
trap - EXIT
if [ "$st" -ne 0 ]; then
  echo "==> go build failed (exit $st)"
  exit "$st"
fi

echo "==> Copying to dist/windows/bin (next to runtime DLLs)..."
mkdir -p "$DIST_DIR"
cp "$EXE_OUT" "$DIST_EXE"
if ! ls "$DIST_DIR"/avutil-*.dll >/dev/null 2>&1; then
  echo "==> WARNING: no avutil-*.dll in $DIST_DIR"
  echo "    Run ./scripts/build_windows.sh once so FFmpeg/OpenSSL/opus DLLs are bundled."
  echo "    Until then this exe will fail with avutil/avcodec/libcrypto/libopus missing."
fi
echo "==> Done: $DIST_EXE"
echo "    Launch via dist/windows/USBridge_Client.lnk or this bin\\ exe, not dist/windows/USBridge_Client.exe"
ls -lh "$DIST_EXE"
