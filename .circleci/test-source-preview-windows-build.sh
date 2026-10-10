#!/usr/bin/env bash
# Native viewer build/privacy gate. It never starts a stream or opens a desktop.
# Only the small closed JSON receipt is published, never binaries or source.
set -euo pipefail
if [[ -n "${USBRIDGE_CI_ROOT:-}" ]]; then cd "$(cygpath -u "$USBRIDGE_CI_ROOT")"; fi
ROOT="$PWD"; WORK="$ROOT/.source-preview-windows-build"; OUT="$ROOT/artifacts/windows-preview-evidence"
export PATH="/c/ci/go/bin:/ucrt64/bin:/usr/bin:$PATH" CC=gcc CXX=g++ CGO_ENABLED=1 GOTOOLCHAIN=local
# These exact directories are used only by this public client build step.
export GOPATH='C:\ci\public-viewer-gopath' GOCACHE='C:\ci\public-viewer-go-build' GOMODCACHE='C:\ci\public-viewer-modules' GOROOT='C:\ci\go'
export GOPROXY=https://proxy.golang.org GOSUMDB=sum.golang.org GOPRIVATE= GONOPROXY= GONOSUMDB=
export GOENV=off GOWORK=off GOFLAGS='-trimpath -mod=readonly' GOCACHEPROG= GOEXPERIMENT=
export CGO_CFLAGS='-O2 -g' CGO_CXXFLAGS='-O2 -g' CGO_CPPFLAGS= CGO_LDFLAGS='-O2 -g'
export CMAKE_GENERATOR=Ninja PKG_CONFIG=pkg-config PKG_CONFIG_LIBDIR=/ucrt64/lib/pkgconfig
[[ "$(go version)" == 'go version go1.26.9 windows/amd64' ]]
[[ "$(git rev-parse HEAD)" == "${CIRCLE_SHA1:?exact source commit required}" ]]
mkdir -p "$WORK/bin" "$WORK/tests" "$OUT"
# Dependencies were installed before exact-key cache restore. Refuse a changed
# package inventory rather than using compilation units from another toolchain.
[[ -s "$WORK/public-cache-key.txt" ]]
cmp <(pacman -Q) "$WORK/packages.txt"
PIN=a232e27d5c423eb8e7de8da91f0eefcf9171348f
# Windows checkout configuration can rewrite HTTPS to SSH. This dependency is
# public: ignore only command-scoped global/system Git configuration and use
# its reviewed HTTPS URL. Do not disable SSH/TLS verification or add credentials.
: > "$WORK/public-git.config"
GIT_CONFIG_NOSYSTEM=1 GIT_CONFIG_GLOBAL="$(cygpath -m "$WORK/public-git.config")" GIT_CONFIG_COUNT=0 \
 git -c credential.helper= -c submodule.client/moonlight-common-c.url=https://github.com/itsme228/moonlight-common-c.git \
 submodule update --init --recursive client/moonlight-common-c
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
 # Native dependencies are built before all tests; this cache is fresh per job.
 # Reuse those exact trimmed-path compilation units instead of rebuilding all.
 go build -trimpath -ldflags='-H=windowsgui' -o "$WORK/bin/source-preview-viewer.exe" ./cmd/source-preview-viewer
)
python3 .circleci/source/windows_preview_build_receipt.py \
 --work "$(cygpath -m "$WORK")" --output "$(cygpath -m "$OUT/build.json")" \
 --ucrt-bin "$(cygpath -m /ucrt64/bin)" --commit "$CIRCLE_SHA1" --public-client-pin "$PIN"
echo 'Native Windows viewer build and protocol/privacy tests passed; streaming/rendering is a separate gate.'
