#!/usr/bin/env bash
set -euo pipefail

ROOT="$(git rev-parse --show-toplevel)"
OUT="$ROOT/artifacts/web-client"
mkdir -p "$OUT"

# Probe the actual executor before downloading/building. gzip archives avoid an
# incidental xz dependency; no privileged package installation is needed.
for tool in bash curl tar gzip git python3 sha256sum; do
  command -v "$tool" >/dev/null || { echo "Required web build tool missing: $tool" >&2; exit 1; }
done
python3 -c 'import sys; assert sys.version_info >= (3, 10), "Python 3.10+ required"'

# Explicit local override lets developers use an already verified Go toolchain.
# CI downloads the pinned archive into a disposable directory.
TOOLS="$(mktemp -d)"
trap 'rm -rf "$TOOLS"' EXIT
if [[ -n "${USBRIDGE_WEB_GO:-}" ]]; then
  GO_BIN_DIR="$(dirname "$USBRIDGE_WEB_GO")"
  export PATH="$GO_BIN_DIR:$PATH"
else
  ARCHIVE=go1.26.6.linux-amd64.tar.gz
  HASH=708effb774be8237570d0add163225abbdfaf4fca28b2611df167beba4feef89
  curl --retry 3 -fsSL "https://go.dev/dl/$ARCHIVE" -o "$TOOLS/$ARCHIVE"
  (cd "$TOOLS" && printf '%s  %s\n' "$HASH" "$ARCHIVE" | sha256sum -c -)
  tar -xzf "$TOOLS/$ARCHIVE" -C "$TOOLS"
  export PATH="$TOOLS/go/bin:$PATH"
  printf '%s  %s\n' "$HASH" "$ARCHIVE" > "$OUT/go-toolchain-sha256.txt"
fi
export GOTOOLCHAIN=local GOFLAGS=-mod=readonly
[[ "$(go version)" == "go version go1.26.6 "* ]] || { echo "Go 1.26.6 required" >&2; exit 1; }

# Node runs browser API mocks, not real browser/agent streaming acceptance.
# Record the image's compatible Node, or use a fixed verified fallback.
if ! command -v node >/dev/null || ! node -e 'process.exit(Number(process.versions.node.split(".")[0]) >= 18 ? 0 : 1)'; then
  ARCHIVE=node-v22.15.0-linux-x64.tar.gz
  HASH=29d1c60c5b64ccdb0bc4e5495135e68e08a872e0ae91f45d9ec34fc135a17981
  curl --retry 3 -fsSL "https://nodejs.org/download/release/v22.15.0/$ARCHIVE" -o "$TOOLS/$ARCHIVE"
  (cd "$TOOLS" && printf '%s  %s\n' "$HASH" "$ARCHIVE" | sha256sum -c -)
  tar -xzf "$TOOLS/$ARCHIVE" -C "$TOOLS"
  export PATH="$TOOLS/node-v22.15.0-linux-x64/bin:$PATH"
  printf '%s  %s\n' "$HASH" "$ARCHIVE" > "$OUT/node-toolchain-sha256.txt"
fi

cd "$ROOT/client"
go mod download
go mod verify | tee "$OUT/module-verification.txt"
go list -m all > "$OUT/modules.txt"
go test ./internal/webrtcweb ./pkg/capabilities ./internal/models | tee "$OUT/tests-native.txt"
CGO_ENABLED=0 GOOS=js GOARCH=wasm go test \
  -exec "$(go env GOROOT)/lib/wasm/go_js_wasm_exec" ./internal/webrtcweb ./pkg/capabilities ./internal/models \
  | tee "$OUT/tests-wasm.txt"
CGO_ENABLED=0 GOOS=js GOARCH=wasm go vet ./cmd/wasm ./internal/webrtcweb \
  2>&1 | tee "$OUT/vet-wasm.txt"
bash scripts/build_web.sh 2>&1 | tee "$OUT/build.txt"
node --test web/runtime-policy.test.cjs
node --check web/runtime-policy.js
node --check web/runtime-config.js
node --check web/bootstrap.js
node --input-type=module --check < web/ai_vision.js
for module in web/vendor/ort/*.mjs; do node --check "$module"; done
node -e 'const fs = require("fs"); WebAssembly.compile(fs.readFileSync("web/app.wasm")).then(() => console.log("app.wasm compiles in Node"), err => { console.error(err); process.exitCode = 1; });' \
  | tee "$OUT/wasm-compile.txt"

cd "$ROOT"
python3 -m unittest discover -s .circleci -p 'test_package_web.py' -v \
  2>&1 | tee "$OUT/tests-packaging.txt"
python3 .circleci/package_web.py "$ROOT" "$OUT"
