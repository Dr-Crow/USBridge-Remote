#!/usr/bin/env bash
# Native viewer build/privacy gate. It never starts a stream or opens a desktop.
# Only the small closed JSON receipt is published, never binaries or source.
set -euo pipefail
if [[ -n "${USBRIDGE_CI_ROOT:-}" ]]; then cd "$(cygpath -u "$USBRIDGE_CI_ROOT")"; fi
ROOT="$PWD"; WORK="$ROOT/.source-preview-windows-build"; OUT="$ROOT/artifacts/windows-preview-evidence"
export PATH="/c/ci/go/bin:/ucrt64/bin:/usr/bin:$PATH" CC=gcc CXX=g++ CGO_ENABLED=1 GOTOOLCHAIN=local
export GOPATH='C:\ci\gopath' GOCACHE='C:\ci\go-build'
export CMAKE_GENERATOR=Ninja PKG_CONFIG=pkg-config PKG_CONFIG_LIBDIR=/ucrt64/lib/pkgconfig
[[ "$(go version)" == 'go version go1.26.9 windows/amd64' ]]
[[ "$(git rev-parse HEAD)" == "${CIRCLE_SHA1:?exact source commit required}" ]]
mkdir -p "$WORK/bin" "$WORK/tests" "$OUT"
pacman --noconfirm -S --needed mingw-w64-ucrt-x86_64-cmake mingw-w64-ucrt-x86_64-ninja \
 mingw-w64-ucrt-x86_64-pkgconf mingw-w64-ucrt-x86_64-opus mingw-w64-ucrt-x86_64-openssl \
 mingw-w64-ucrt-x86_64-ffmpeg mingw-w64-ucrt-x86_64-vulkan-headers mingw-w64-ucrt-x86_64-vulkan-loader
pacman -Q > "$WORK/packages.txt"
PIN=a232e27d5c423eb8e7de8da91f0eefcf9171348f
git submodule update --init --recursive client/moonlight-common-c
[[ "$(git -C client/moonlight-common-c rev-parse HEAD)" == "$PIN" ]]
(
 cd client
 pkg-config --exists opus openssl libavcodec libavutil libswscale vulkan
 go mod download
 # These existing public-repository scripts use the pinned submodule and the
 # already-vendored codec. No private repository or replacement source is fetched.
 bash scripts/build_moonlight.sh
 bash scripts/build_pyrowave_windows.sh
 go test -json -count=1 ./internal/sourcepreview | tee "$WORK/tests/descriptor.jsonl"
 go test -json -count=1 ./internal/service -run '^(TestSourcePreview|TestDisconnect)' | tee "$WORK/tests/service.jsonl"
 go test -json -count=1 ./cmd/source-preview-viewer | tee "$WORK/tests/command.jsonl"
 go test -json -count=1 -ldflags='-H=windowsgui' ./cmd/source-preview-viewer | tee "$WORK/tests/command-gui.jsonl"
 go build -a -trimpath -ldflags='-H=windowsgui' -o "$WORK/bin/source-preview-viewer.exe" ./cmd/source-preview-viewer
)
python3 .circleci/source/windows_preview_build_receipt.py \
 --work "$(cygpath -m "$WORK")" --output "$(cygpath -m "$OUT/build.json")" \
 --ucrt-bin "$(cygpath -m /ucrt64/bin)" --commit "$CIRCLE_SHA1" --public-client-pin "$PIN"
echo 'Native Windows viewer build and protocol/privacy tests passed; streaming/rendering is a separate gate.'
