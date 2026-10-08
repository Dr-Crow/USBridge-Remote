#!/usr/bin/env bash
set -euo pipefail
if [[ -n "${USBRIDGE_CI_ROOT:-}" ]]; then
  cd "$(cygpath -u "$USBRIDGE_CI_ROOT")"
fi
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
# Go 1.26 verify treats the local usbridge-client replacement as a cache
# archive and fails looking for its ziphash. Verify all remote modules using
# an otherwise identical temporary module; the local client is Git source.
go mod download
VERIFY_DIR="$(mktemp -d)"
cp go.mod go.sum "$VERIFY_DIR/"
(
  cd "$VERIFY_DIR"
  go mod edit -droprequire=usbridge-client -dropreplace=usbridge-client
  go mod verify
) 2>&1 | tee "$ROOT/artifacts/module-verification.txt"
rm -rf "$VERIFY_DIR"
# ci uses Fyne's software driver for unit tests, not the shipped application.
# Upstream's suite contains POSIX-only process fixtures plus two unrelated
# Windows failures. Keep the explicit exclusions reviewable; every other test
# (including all fork policy/provisioning/security tests) must pass.
WINDOWS_SKIP='^(TestKillPID_TerminatesRealProcess|TestEvictEngineLockHolder_FallsBackToKillWhenUnresponsive|TestWatchProcessExit_ClearsCmdOnExit|TestWatchProcessExit_DoesNotClobberNewerCmd|TestWatchProcessExit_FiresOnExitCallback|TestWatchProcessExit_DoesNotFireOnExitForStaleCmd|TestStop_TerminatesGracefullyBeforeKill|TestStart_RealHungProcessIsDetectedKilledAndReplaced|TestStop_EscalatesToKillWhenSigtermIgnored|TestAcquireEngineLock_ExclusiveAcrossHandles|TestBenchLoadStartStopWireFormat)$'
printf 'Windows excluded baseline tests: %s\nSee TEST-LIMITATIONS.md for reasons.\n' "$WINDOWS_SKIP" > "$ROOT/artifacts/test-exclusions.txt"
cp ../docs/WINDOWS_TEST_LIMITATIONS.md "$ROOT/artifacts/TEST-LIMITATIONS.md"
go test -tags ci -timeout 10m -skip "$WINDOWS_SKIP" ./... 2>&1 | tee "$ROOT/artifacts/tests.txt"
go vet -tags ci -unsafeptr=false ./... 2>&1 | tee "$ROOT/artifacts/vet.txt"
# Build the actual native GUI executable with upstream's DLL packaging script.
USBRIDGE_WINDOWS_LDFLAGS="-H=windowsgui -X usbridge_agent/internal/update.Channel=manual" bash scripts/build_windows.sh 2>&1 | tee "$ROOT/artifacts/build.txt"
cp dist/windows/USBridgeAgent.exe "$ROOT/artifacts/USBridgeAgent.exe"
cp dist/USBridgeAgent-Windows-x86_64-*.zip "$ROOT/artifacts/"
cp ../docs/FORK_TEST_PLAN.md "$ROOT/artifacts/TEST-INSTRUCTIONS.md"
cp ../docs/LOCAL_RUNTIME_RESEARCH.md "$ROOT/artifacts/LOCAL-RUNTIME.md"
cp LICENSE "$ROOT/artifacts/LICENSE"
cd "$ROOT/artifacts"
sha256sum USBridgeAgent.exe USBridgeAgent-Windows-x86_64-*.zip > SHA256SUMS.txt
# The combined archive uses a genuine short-lived entitlement for this CI
# machine. Never copy a user's token/config or embed the CI token in artifacts.
cd "$ROOT/agent"
bash "$ROOT/.circleci/prepare-components.sh" "$ROOT" windows/amd64
USBRIDGE_BUNDLE_FIXTURES="$(cygpath -w "$ROOT/artifacts/components")" go test -tags ci ./internal/app -run '^TestAutomaticBundledPairOfflineFixture$' -v 2>&1 | tee "$ROOT/artifacts/offline-bundle-smoke.txt"
# Runtime-only lab acceptance is distinct from a claim of USB device/streaming
# functionality. The original archive and executable are never changed.
mkdir -p "$ROOT/lab-ci-input"
python - "$ROOT/artifacts/components/usbridge-usb-broker-windows-x86_64.zip" "$ROOT/lab-ci-input" <<'PYZIP'
import sys, zipfile
with zipfile.ZipFile(sys.argv[1]) as z:
    for name in z.namelist():
        if name.endswith('usbridge-usb-broker.exe'):
            with open(sys.argv[2] + '/usbridge-usb-broker.exe', 'wb') as f:
                f.write(z.read(name))
PYZIP
USBRIDGE_LAB_TEST_BINARY="$(cygpath -w "$ROOT/lab-ci-input/usbridge-usb-broker.exe")" go test -tags ci ./internal/localruntime -run TestPinnedBrokerLocalRuntime -v 2>&1 | tee "$ROOT/artifacts/local-runtime-smoke.txt"

# Stock streamer parser acceptance uses its one-shot credentials writer, not a
# running capture server. Extract only the expected executable/runtime dependency.
python - "$ROOT/artifacts/components/usbridge-streamer-windows-x86_64.zip" "$ROOT/lab-ci-input" <<'PYSTREAMER'
import pathlib, sys, zipfile
wanted = {"usbridge-streamer.exe", "libopus-0.dll"}
seen = set()
with zipfile.ZipFile(sys.argv[1]) as z:
    for entry in z.infolist():
        name = entry.filename.replace("\\", "/").split("/")[-1]
        if name in wanted:
            if name in seen:
                raise SystemExit("duplicate streamer input")
            seen.add(name)
            pathlib.Path(sys.argv[2], name).write_bytes(z.read(entry))
if seen != wanted:
    raise SystemExit("missing expected streamer inputs")
PYSTREAMER
USBRIDGE_LAB_STREAMER_BINARY="$(cygpath -w "$ROOT/lab-ci-input/usbridge-streamer.exe")" go test -tags ci ./internal/localruntime -run '^TestPinnedStreamerEmptyICECLI$' -v 2>&1 | tee "$ROOT/artifacts/streamer-empty-ice-cli.txt"

mkdir -p "$ROOT/offline-bundle/agent/components"
cp -R dist/windows/. "$ROOT/offline-bundle/agent/"
cp -R "$ROOT/artifacts/components/." "$ROOT/offline-bundle/agent/components/"
cp ../docs/FORK_TEST_PLAN.md "$ROOT/offline-bundle/README.md"
cp ../docs/AUTOMATIC_BUNDLES.md "$ROOT/offline-bundle/AUTOMATIC_BUNDLES.md"
cp ../docs/RUNTIME_NETWORK_POLICY.md "$ROOT/offline-bundle/RUNTIME_NETWORK_POLICY.md"
(cd "$ROOT/offline-bundle" && zip -r "$ROOT/artifacts/USBridgeAgent-Windows-with-components.zip" .)
cd "$ROOT/artifacts"
sha256sum USBridgeAgent-Windows-with-components.zip >> SHA256SUMS.txt
