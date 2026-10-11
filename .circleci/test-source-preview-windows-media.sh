#!/usr/bin/env bash
# Generated inputs only. The runtime fixture cannot capture, launch an encoder,
# contact a network, or read a descriptor. Do not substitute real FFmpeg at launch.
set -euo pipefail
if [[ -n "${USBRIDGE_CI_ROOT:-}" ]]; then cd "$(cygpath -u "$USBRIDGE_CI_ROOT")"; fi
ROOT="$PWD"; WORK="$ROOT/.source-preview-windows-build"; OUT="$ROOT/artifacts/windows-preview-evidence"
export PATH="/c/ci/go/bin:/ucrt64/bin:/usr/bin:$PATH" CC=gcc CXX=g++ CGO_ENABLED=1 GOTOOLCHAIN=local
export GOPATH='C:\ci\gopath' GOCACHE='C:\ci\go-build'
[[ "$(go version)" == 'go version go1.26.9 windows/amd64' ]]
[[ "$(git rev-parse HEAD)" == "${CIRCLE_SHA1:?}" ]]
mkdir -p "$WORK/fixture" "$WORK/components/bin" "$WORK/agent" "$WORK/tests" "$OUT"
python3 - "$WORK" "$OUT" <<'PY'
import hashlib,json,pathlib,sys
work,out=map(pathlib.Path,sys.argv[1:]);r=json.loads((out/'build.json').read_text())
assert r['passed'] and r['viewer_compiled']
assert hashlib.sha256((work/'bin/source-preview-viewer.exe').read_bytes()).hexdigest()==r['viewer_sha256']
PY
# The saved public cache is read-only input to this unsaved job-local cache.
PYTHONPATH=.circleci/source python3 -m unittest discover -s .circleci/source -p 'test_windows_seed_public_cache.py'
python3 .circleci/source/windows_seed_public_cache.py --root "$(cygpath -m "$ROOT")"
bash .circleci/test-source-preview-windows-software-gl.sh
# Fail on the actual executable's window creation before source/agent builds.
(
 cd .circleci/source/windows-preview-acceptance
 CGO_ENABLED=0 go test -count=1 -timeout=2m ./...
 CGO_ENABLED=0 go build -trimpath -o "$WORK/windows-preview-acceptance.exe" .
)
PYTHONPATH=.circleci/source python3 -m unittest discover -s .circleci/source -p 'test_check_windows_viewer_startup.py'
python3 .circleci/source/check_windows_viewer_startup.py \
 --work "$(cygpath -m "$WORK")" --output "$(cygpath -m "$OUT")" \
 --runner "$(cygpath -m "$WORK/windows-preview-acceptance.exe")" --commit "$CIRCLE_SHA1"
(
 cd .circleci/source/windows-preview-fixture
 CGO_ENABLED=0 go test -json -count=1 -timeout=2m ./... | tee "$WORK/tests/fixture.jsonl"
 CGO_ENABLED=0 go vet ./...
 python3 build_assets.py --ffmpeg "$(cygpath -m /ucrt64/bin/ffmpeg.exe)" --output "$(cygpath -m "$WORK/fixture")"
 CGO_ENABLED=0 go build -trimpath -ldflags "-H=windowsgui $(cat "$WORK/fixture/fixture-ldflags.txt")" -o "$WORK/fixture/ffmpeg-fixture.exe" .
 python3 verify_fixture.py --ffmpeg "$(cygpath -m /ucrt64/bin/ffmpeg.exe)" --fixture "$(cygpath -m "$WORK/fixture/ffmpeg-fixture.exe")" > "$OUT/generated-encoder.json"
)
# Extract only the unchanged snapshot that already exists in this public tree.
# No private repository is contacted and no source-containing artifact is emitted.
python3 - "$ROOT" "$WORK" <<'PY'
import hashlib,json,pathlib,sys,tarfile
root,work=map(pathlib.Path,sys.argv[1:]);snap=root/'.circleci/source-snapshots'
meta=json.loads((snap/'source-streamer.json').read_text());archive=snap/meta['archive']
assert meta['commit']=='2e07af3484369bc68bc8091d5a04969867eff0f9'
assert meta['archive_sha256']=='7a8ec9b04f0b3f090de55dc6fc7194e7bd7caf93a08a11182c7c03dcfce1ab2b'
assert hashlib.sha256(archive.read_bytes()).hexdigest()==meta['archive_sha256']
target=work/'streamer';target.mkdir(exist_ok=True)
with tarfile.open(archive) as t:
 members=t.getmembers()
 assert len(members)==meta['file_count']==140 and sum(m.size for m in members)<16<<20
 assert all(m.isfile() and m.name.startswith('source/') and '..' not in pathlib.PurePosixPath(m.name).parts for m in members)
 t.extractall(target,filter='data')
PY
(
 cd "$WORK/streamer/source"
 go test -timeout 5m ./... | tee "$WORK/tests/source-streamer.txt"
 go build -trimpath -ldflags '-s -w -X main.version=2e07af3484369bc68bc8091d5a04969867eff0f9' -o "$WORK/components/bin/source-streamer.exe" ./cmd/source-streamer
)
(
 cd agent
 # Loopback-only engine ownership regressions, compiled with the full App.
 go test -json -tags ci -count=20 -timeout=3m -run '^Test(USBBridge|HTTPServerOwner|AppRunCannotReuseRetiredOwner|PreviewOwner|PreviewCloseAccounting|EngineWorkers|SocketWorkers|RelayWebsocketBroker|MouseAndClipboardRoutes|LegacyAttach|MouseOldOwner|Clipboard|Admin)' ./internal/api ./internal/app ./internal/adminapi | tee "$WORK/tests/engine-owners.jsonl"
 go test -json -count=5 -timeout=3m ./internal/sourcestreamer | tee "$WORK/tests/source-supervisor.jsonl"
 go build -trimpath -ldflags '-H=windowsgui -X usbridge_agent/internal/update.Channel=manual' -o "$WORK/agent/USBridgeAgent.exe" ./cmd/usbridge_agent
)
# Native helper tests establish the Job Object and pipe prerequisites before
# the actual encrypted-stream, changing-pixel and cleanup gate.
(
 cd .circleci/source/windows-preview-acceptance
 CGO_ENABLED=0 go test -json -count=1 -timeout=2m ./... | tee "$WORK/tests/acceptance.jsonl"
 CGO_ENABLED=0 go vet ./...
 CGO_ENABLED=0 go build -trimpath -o "$WORK/windows-preview-acceptance.exe" .
)
python3 .circleci/source/prepare_windows_preview_media.py \
 --work "$(cygpath -m "$WORK")" --output "$(cygpath -m "$OUT")" \
 --ucrt-bin "$(cygpath -m /ucrt64/bin)" --runner "$(cygpath -m "$WORK/windows-preview-acceptance.exe")" --commit "$CIRCLE_SHA1"
# Prepare the production Manager's exact two-role package only after the full
# CLI pixel/cleanup gate passed. This does not bypass an elevated CI token or
# enable the still-gated Windows settings action.
PYTHONPATH=.circleci/source python3 -m unittest discover -s .circleci/source -p 'test_prepare_windows_preview_manager.py'
python3 .circleci/source/prepare_windows_preview_manager.py \
 --work "$(cygpath -m "$WORK")" --output "$(cygpath -m "$OUT")" --commit "$CIRCLE_SHA1"
(
 cd agent
 CGO_ENABLED=0 go test -json -count=1 -timeout=2m -tags=source_preview_manager_acceptance \
  '-run=^TestWindowsManagerAcceptance' ./internal/sourcepreview > "$WORK/tests/manager-preparation.jsonl"
 CGO_ENABLED=0 go vet -tags=source_preview_manager_acceptance ./internal/sourcepreview
)
PYTHONPATH=.circleci/source python3 - "$WORK" "$OUT" "$CIRCLE_SHA1" <<'PYMANAGER'
import hashlib,json,pathlib,sys
from windows_preview_build_receipt import summarize_tests
work,out=map(pathlib.Path,sys.argv[1:3]);commit=sys.argv[3]
required={'TestWindowsManagerAcceptancePinsAndRoles','TestWindowsManagerAcceptancePixelAndPathDecisions','TestWindowsManagerAcceptanceCleanupAccounting'}
summary=summarize_tests(work/'tests/manager-preparation.jsonl',required)
staging=json.loads((out/'manager-staging.json').read_bytes())
if staging['commit']!=commit or not staging['passed'] or staging['production_manager_executed']:
 raise SystemExit('Exact nonexecuting Manager staging proof required')
receipt={'schema_version':1,'commit':commit,'passed':True,'native_helper_tests':summary,'native_vet':True,
 'manager_staging_receipt_sha256':hashlib.sha256((out/'manager-staging.json').read_bytes()).hexdigest(),
 'actual_manager_started':False,'actual_app_new':False,'desktop_capture_tested':False,
 'identity_or_security_changes':False,'source_or_binary_artifacts_published':False}
(out/'manager-preparation.json').write_text(json.dumps(receipt,indent=2)+'\n')
PYMANAGER
