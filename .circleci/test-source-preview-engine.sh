#!/usr/bin/env bash
# Additive gate AFTER the unchanged source preview manager and dialog gates.
# No source fetch, no private repositories, no package/archive publication.
set -euo pipefail
unset SOURCE_PREVIEW_NATIVE_ACCEPTANCE SOURCE_PREVIEW_DIALOG_ACCEPTANCE SOURCE_PREVIEW_ENGINE_ACCEPTANCE
ROOT="$PWD"
OUT="$ROOT/artifacts/source-preview-engine"
export PATH="$HOME/ci-toolchain/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=1
[[ "$(go version)" == 'go version go1.26.9 linux/amd64' ]]
COMMIT="$(git rev-parse HEAD)"
[[ "$COMMIT" == "${CIRCLE_SHA1:?exact hosted commit required}" ]]
command -v docker >/dev/null
mkdir -p "$OUT"
WORK="$(mktemp -d "$ROOT/.source-preview-engine.XXXXXX")"
IMAGE="usbridge-preview-engine:${COMMIT:0:12}-$$"
CONTAINER="usbridge-preview-engine-$$"
cleanup() {
 docker rm -f "$CONTAINER" >/dev/null 2>&1 || true
 docker image rm "$IMAGE" >/dev/null 2>&1 || true
 rm -rf "$WORK"
}
trap cleanup EXIT
mkdir -p "$WORK/context/agent" "$WORK/context/gate"
# Plain and observed builds both use the actual shipped command and full engine.
# The CI software Fyne driver is NEVER used in these two executables.
(cd agent && go build -trimpath -o "$WORK/context/agent/usbridge-agent" ./cmd/usbridge_agent)
(cd agent && go build -trimpath -tags source_preview_engine_acceptance -o "$WORK/context/agent/usbridge-agent-observed" ./cmd/usbridge_agent)
(cd agent && go test -race -tags ci,source_preview_engine_acceptance -run '^TestEnginePreview' ./internal/ui ./internal/app)
python3 -m unittest discover -s .circleci/source -p test_engine_preview.py -v
python3 -m unittest discover -s .circleci/source -p test_preview_processes.py -v
python3 -m unittest discover -s .circleci/source -p test_preview_windows.py -v
for file in engine_preview_container.py engine_preview_contract.py preview_engine.py preview_pixels.py preview_processes.py preview_windows.py engine-preview-config.yaml; do
 cp ".circleci/source/$file" "$WORK/context/gate/"
done
python3 .circleci/source/prepare_engine_preview.py "$ROOT" "$WORK/context" "$COMMIT"
cp .circleci/source/engine-preview.Dockerfile "$WORK/context/Dockerfile"
# Dependency installation is build-time only. Runtime receives no user-supplied host data mounts,
# devices, supplementary groups, credentials, host bus/display, or Docker socket.
# The Dockerfile names the independently verified Jammy amd64 manifest digest.
# Never resolve or pull a mutable base tag in this gate.
docker build --platform linux/amd64 --tag "$IMAGE" "$WORK/context" > "$WORK/image-build.log" 2>&1
RUNTIME_IMAGE_SHA256="$(docker image inspect --format '{{.Id}}' "$IMAGE")"
RUNTIME_IMAGE_SHA256="${RUNTIME_IMAGE_SHA256#sha256:}"
[[ "$RUNTIME_IMAGE_SHA256" =~ ^[a-f0-9]{64}$ ]]
NAMESPACES=()
for ns in pid mnt ipc net uts; do
 NAMESPACES+=(--env "OUTER_${ns^^}_NS=$(readlink "/proc/self/ns/$ns")")
done
docker create --name "$CONTAINER" --network none --read-only \
 --user 10001:10001 --cap-drop ALL --security-opt no-new-privileges=true \
 --ipc private --pids-limit 512 --memory 2g --cpus 2 --init \
 --tmpfs /tmp:rw,nosuid,nodev,noexec,mode=1777,size=128m \
 --tmpfs /tmp/.X11-unix:rw,nosuid,nodev,noexec,mode=1777,uid=0,gid=0,size=1m \
 --tmpfs /run:rw,nosuid,nodev,noexec,mode=0755,size=16m \
 --tmpfs /work:rw,nosuid,nodev,mode=0700,uid=10001,gid=10001,size=768m \
 --env "RUNTIME_IMAGE_SHA256=$RUNTIME_IMAGE_SHA256" "${NAMESPACES[@]}" "$IMAGE" > /dev/null
# Both Xvfb instances, D-Bus, CLI and all four media children are private here.
# A timeout/forced removal cleans up failed work but NEVER yields a passing gate.
timeout --kill-after=15s 300s docker start --attach "$CONTAINER" > "$WORK/receipt.json"
[[ "$(docker inspect --format '{{.State.ExitCode}}' "$CONTAINER")" == 0 ]]
PYTHONPATH="$ROOT/.circleci/source" python3 - "$WORK/receipt.json" "$OUT/result.json" <<'PY'
import json,pathlib,sys
from engine_preview_contract import validate_receipt
result=validate_receipt(json.loads(pathlib.Path(sys.argv[1]).read_text()))
assert result['passed']
pathlib.Path(sys.argv[2]).write_text(json.dumps(result,indent=2,sort_keys=True)+'\n')
print('Real CLI/App.New native preview gate passed; only schema-checked numeric/status receipts retained.')
PY
