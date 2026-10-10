#!/usr/bin/env bash
# Compile and inventory the already-public frozen source before any Fyne build.
# This gate never starts the source runtime, a media session, or a desktop window.
set -euo pipefail
if [[ -n "${USBRIDGE_CI_ROOT:-}" ]]; then cd "$(cygpath -u "$USBRIDGE_CI_ROOT")"; fi
ROOT="$PWD"; WORK="$ROOT/.source-preview-windows-staging"; OUT="$ROOT/artifacts/windows-preview-evidence"
export PATH="/c/ci/go/bin:/ucrt64/bin:/usr/bin:$PATH" CC=gcc CXX=g++ CGO_ENABLED=1 GOTOOLCHAIN=local
export GOPATH='C:\ci\gopath' GOCACHE='C:\ci\go-build'
[[ "$(go version)" == 'go version go1.26.9 windows/amd64' ]]
[[ "$(git rev-parse HEAD)" == "${CIRCLE_SHA1:?}" ]]
[[ ! -e "$WORK" ]]
mkdir -p "$WORK/components/bin" "$WORK/tests" "$OUT"
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
python3 .circleci/source/windows_component_inventory.py \
 --source "$(cygpath -m "$WORK/components/bin/source-streamer.exe")" \
 --ucrt-bin "$(cygpath -m /ucrt64/bin)" --output "$(cygpath -m "$OUT")" --commit "$CIRCLE_SHA1"
