#!/usr/bin/env bash
# Build the experimental same-host Linux viewer using the normal client renderer.
# Run only from a complete checkout with the pinned submodule initialized.
set -euo pipefail
ROOT="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
cd "$ROOT"
[[ "$(uname -s)" == Linux ]] || { echo 'Source preview v1 requires Linux.' >&2; exit 1; }
PIN=a232e27d5c423eb8e7de8da91f0eefcf9171348f
[[ "$(git -C moonlight-common-c rev-parse HEAD 2>/dev/null || true)" == "$PIN" ]] || {
  echo "Initialize client/moonlight-common-c at the reviewed pin $PIN before building." >&2; exit 1;
}
pkg-config --exists opus openssl libavcodec libavutil libswscale libpulse-simple libva libva-drm gl x11
[[ -f third_party/pyrowave/vendor/pyrowave/PUNKTFUNK-VENDOR.txt ]] || { echo 'Missing vendored PyroWave tree.' >&2; exit 1; }
bash scripts/build_moonlight.sh
bash scripts/build_pyrowave.sh
mkdir -p dist
CGO_ENABLED=1 go build -trimpath -o dist/source-preview-viewer ./cmd/source-preview-viewer
sha256sum dist/source-preview-viewer
