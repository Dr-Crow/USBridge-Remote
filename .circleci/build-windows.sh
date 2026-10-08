#!/usr/bin/env bash
set -euo pipefail
ROOT="$(pwd)"
export PATH="/c/ci/go/bin:/ucrt64/bin:/usr/bin:$PATH"
export CC=gcc CXX=g++ CGO_ENABLED=1 GOTOOLCHAIN=local
export GOPATH='C:\ci\gopath' GOCACHE='C:\ci\go-build'
mkdir -p "$ROOT/artifacts"
{
  printf 'Commit: %s\nBranch: %s\n' "${CIRCLE_SHA1:-unknown}" "${CIRCLE_BRANCH:-unknown}"
  go version
  gcc --version
  pacman -Q
} > "$ROOT/artifacts/build-info.txt"
cd agent
go mod verify 2>&1 | tee "$ROOT/artifacts/module-verification.txt"
# ci uses Fyne's software driver for unit tests, not the shipped application.
go test -tags ci -timeout 10m ./... 2>&1 | tee "$ROOT/artifacts/tests.txt"
go vet -tags ci -unsafeptr=false ./... 2>&1 | tee "$ROOT/artifacts/vet.txt"
# Build the actual native GUI executable with upstream's DLL packaging script.
USBRIDGE_WINDOWS_LDFLAGS="-H=windowsgui -X usbridge_agent/internal/update.Channel=manual" bash scripts/build_windows.sh 2>&1 | tee "$ROOT/artifacts/build.txt"
cp dist/windows/USBridgeAgent.exe "$ROOT/artifacts/USBridgeAgent.exe"
cp dist/USBridgeAgent-Windows-x86_64-*.zip "$ROOT/artifacts/"
cp ../docs/FORK_TEST_PLAN.md "$ROOT/artifacts/TEST-INSTRUCTIONS.md"
cp LICENSE "$ROOT/artifacts/LICENSE"
cd "$ROOT/artifacts"
sha256sum USBridgeAgent.exe USBridgeAgent-Windows-x86_64-*.zip > SHA256SUMS.txt
# The combined archive uses a genuine short-lived entitlement for this CI
# machine. Never copy a user's token/config or embed the CI token in artifacts.
cd "$ROOT/agent"
go run ./cmd/component_bundle -out "$ROOT/artifacts/components"
mkdir -p "$ROOT/offline-bundle/agent" "$ROOT/offline-bundle/components"
cp -R dist/windows/. "$ROOT/offline-bundle/agent/"
cp -R "$ROOT/artifacts/components/." "$ROOT/offline-bundle/components/"
cp ../docs/FORK_TEST_PLAN.md "$ROOT/offline-bundle/README.md"
(cd "$ROOT/offline-bundle" && zip -r "$ROOT/artifacts/USBridgeAgent-Windows-with-components.zip" .)
cd "$ROOT/artifacts"
sha256sum USBridgeAgent-Windows-with-components.zip >> SHA256SUMS.txt
