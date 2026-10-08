#!/usr/bin/env bash
set -euo pipefail
ROOT="$(pwd)"
OS="$(uname -s)"
mkdir -p "$HOME/ci-toolchain" "$ROOT/artifacts"
if [[ "$OS" == Darwin ]]; then
  ARCHIVE=go1.26.6.darwin-arm64.tar.gz
  HASH=2dc95ce4675829f2df0e86b28bcef3283635902062a5f0580ca659bf570f3204
else
  ARCHIVE=go1.26.6.linux-amd64.tar.gz
  HASH=708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89
  sudo apt-get update
  sudo apt-get install -y libgl1-mesa-dev xorg-dev libxkbcommon-dev libfuse2 patchelf pkg-config
fi
curl --retry 3 -fsSL "https://go.dev/dl/$ARCHIVE" -o "$HOME/ci-toolchain/$ARCHIVE"
(cd "$HOME/ci-toolchain" && printf '%s  %s\n' "$HASH" "$ARCHIVE" | shasum -a 256 -c -)
tar -xzf "$HOME/ci-toolchain/$ARCHIVE" -C "$HOME/ci-toolchain"
export PATH="$HOME/ci-toolchain/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=1
{
  printf 'Commit: %s\n' "${CIRCLE_SHA1:-unknown}"
  uname -a
  go version
  cc --version
} > "$ROOT/artifacts/build-info.txt"
cd agent
go mod download
VERIFY_DIR="$(mktemp -d)"; cp go.mod go.sum "$VERIFY_DIR/"
(cd "$VERIFY_DIR" && go mod edit -droprequire=usbridge-client -dropreplace=usbridge-client && go mod verify) | tee "$ROOT/artifacts/module-verification.txt"
rm -rf "$VERIFY_DIR"
go test -tags ci -timeout 10m ./... 2>&1 | tee "$ROOT/artifacts/tests.txt"
go vet -tags ci ./... 2>&1 | tee "$ROOT/artifacts/vet.txt"
VERSION="$(tr -d ' \r\n' < VERSION)"
if [[ "$OS" == Darwin ]]; then
  for TARGET in arm64 amd64; do
    export GOOS=darwin GOARCH="$TARGET" MACOSX_DEPLOYMENT_TARGET=12.3
    TARGET_CPU=arm64; [[ "$TARGET" != amd64 ]] || TARGET_CPU=x86_64
    export CGO_CFLAGS="-arch $TARGET_CPU -mmacosx-version-min=12.3"
    export CGO_LDFLAGS="-arch $TARGET_CPU -mmacosx-version-min=12.3"
    export USBRIDGE_SKIP_SUNSHINE=1
    export USBRIDGE_TAILSCALE_CLI="$ROOT/tailscale-$TARGET"
    TS_MODULE="$(go list -m -f '{{.Dir}}' tailscale.com)"
    (cd "$TS_MODULE" && CGO_ENABLED=0 go build -mod=readonly -trimpath -o "$USBRIDGE_TAILSCALE_CLI" ./cmd/tailscale)
    USBRIDGE_MACOS_LDFLAGS='-X usbridge_agent/internal/update.Channel=manual' bash scripts/build_macos.sh 2>&1 | tee "$ROOT/artifacts/build-$TARGET.txt"
    lipo dist/macos/USBridgeAgent.app/Contents/MacOS/USBridgeAgent -verify_arch "$TARGET_CPU"
    ditto -c -k --sequesterRsrc --keepParent dist/macos/USBridgeAgent.app "$ROOT/artifacts/USBridgeAgent-macOS-$TARGET-$VERSION.zip"
    cp dist/USBridgeAgent-macOS-*.dmg "$ROOT/artifacts/USBridgeAgent-macOS-$TARGET-$VERSION.dmg"
  done
else
  USBRIDGE_LINUX_LDFLAGS='-X usbridge_agent/internal/update.Channel=manual' bash scripts/build_linux.sh 2>&1 | tee "$ROOT/artifacts/build-linux-amd64.txt"
  cp dist/*.AppImage "$ROOT/artifacts/"
  tar -czf "$ROOT/artifacts/USBridgeAgent-Linux-amd64-$VERSION.tar.gz" -C dist/linux usbridge-agent usbridge-streamer-launch
fi
cp ../docs/PLATFORM_BUILDS.md "$ROOT/artifacts/README.md"
cp LICENSE "$ROOT/artifacts/LICENSE"
cd "$ROOT/artifacts"
find . -maxdepth 1 -type f ! -name SHA256SUMS.txt -exec shasum -a 256 '{}' \; > SHA256SUMS.txt
