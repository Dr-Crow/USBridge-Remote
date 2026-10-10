#!/usr/bin/env bash
# Actual native Windows agent/source subprocess acceptance. Never sends PLAY.
set -euo pipefail
if [[ -n "${USBRIDGE_CI_ROOT:-}" ]]; then cd "$(cygpath -u "$USBRIDGE_CI_ROOT")"; fi
ROOT="$(pwd)"; OUT="$ROOT/artifacts/source-components-windows"; WORK="$ROOT/.source-components-windows-ci"
export PATH="/c/ci/go/bin:/ucrt64/bin:/usr/bin:$PATH" CC=gcc CXX=g++ CGO_ENABLED=1 GOTOOLCHAIN=local
export GOPATH='C:\ci\gopath' GOCACHE='C:\ci\go-build'
[[ "$(go env GOOS)/$(go env GOARCH)" == windows/amd64 ]]
[[ "$(go version)" == 'go version go1.26.6 windows/amd64' ]]
STREAMER_COMMIT=2e07af3484369bc68bc8091d5a04969867eff0f9
BROKER_COMMIT=176801ad075ad4d5e503d913f683948980baae2d
mkdir -p "$WORK" "$OUT/package/agent" "$OUT/package/components/bin" "$OUT/package/source"
python3 - "$ROOT" "$WORK" "$STREAMER_COMMIT" "$BROKER_COMMIT" <<'PY'
import hashlib,json,pathlib,sys,tarfile
root,work=map(pathlib.Path,sys.argv[1:3]);snap=root/'.circleci/source-snapshots'
for name,pin in zip(('streamer','broker'),sys.argv[3:]):
    meta=json.loads((snap/f'source-{name}.json').read_text());archive=snap/meta['archive']
    assert meta['commit']==pin and hashlib.sha256(archive.read_bytes()).hexdigest()==meta['archive_sha256']
    target=work/name;target.mkdir(parents=True,exist_ok=True)
    with tarfile.open(archive) as t:
        members=t.getmembers()
        assert len(members)==meta['file_count']
        assert all(m.isfile() and m.name.startswith('source/') and '..' not in pathlib.PurePosixPath(m.name).parts for m in members)
        t.extractall(target)
PY
(cd "$WORK/streamer/source" && go test -timeout 5m ./... && go build -trimpath -ldflags "-s -w -X main.version=$STREAMER_COMMIT" -o "$OUT/package/components/bin/source-streamer.exe" ./cmd/source-streamer) 2>&1 | tee "$OUT/source-streamer-build-tests.txt"
(cd "$WORK/broker/source" && CGO_ENABLED=0 go test -timeout 5m ./... && CGO_ENABLED=0 go build -trimpath -ldflags "-s -w -X main.version=$BROKER_COMMIT" -o "$OUT/package/components/bin/broker-session.exe" ./cmd/broker-session) 2>&1 | tee "$OUT/source-broker-build-tests.txt"
cp -R "$ROOT/agent/dist/windows/." "$OUT/package/agent/"
cp "$ROOT/.circleci/source-snapshots/"* "$OUT/package/source/"
cp "$WORK/streamer/source/LICENSE" "$OUT/package/source/STREAMER-LICENSE"
cp "$WORK/broker/source/LICENSE" "$OUT/package/source/BROKER-LICENSE"
python3 - "$OUT/package/components" "$STREAMER_COMMIT" "$BROKER_COMMIT" <<'PY'
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1]);components=[]
for name,entry,version in [('source-streamer','bin/source-streamer.exe',sys.argv[2]),('source-broker','bin/broker-session.exe',sys.argv[3])]:
    data=(root/entry).read_bytes()
    components.append({'name':name,'platform':'windows/amd64','version':version,'profile':name+'-v1','entry':entry,'files':[{'path':entry,'size':len(data),'sha256':hashlib.sha256(data).hexdigest(),'executable':True}]})
raw=(json.dumps({'schema':1,'components':components},indent=2)+'\n').encode();(root/'manifest.json').write_bytes(raw)
(root/'MANIFEST.sha256').write_text(hashlib.sha256(raw).hexdigest()+'  manifest.json\n')
PY
CGO_ENABLED=0 go build -o "$WORK/windows-lifecycle.exe" "$ROOT/.circleci/source/windows_lifecycle.go"
"$WORK/windows-lifecycle.exe" "$(cygpath -m "$OUT/package/agent/USBridgeAgent.exe")" "$(cygpath -m "$OUT/package/components")" "$(cygpath -m "$OUT/lifecycle.json")"
(cd "$WORK/broker/source" && CGO_ENABLED=0 AGENT_SOURCE_BINARY="$(cygpath -m "$OUT/package/agent/USBridgeAgent.exe")" BROKER_SESSION_BINARY="$(cygpath -m "$OUT/package/components/bin/broker-session.exe")" go test -count=5 -run '^(TestPortableSessionExecutableMutualTLS|TestPortableAgentBrokerSessionMutualTLS)$' -v ./brokersession) 2>&1 | tee "$OUT/agent-broker-mutual-tls.txt"
cp "$ROOT/docs/SOURCE_COMPONENT_PACKAGE.md" "$OUT/package/README.md"
cp "$ROOT/docs/SOURCE_STREAMER_PROTOCOL.md" "$ROOT/docs/SOURCE_BROKER_PROTOCOL.md" "$OUT/package/"
python3 - "$OUT" "${CIRCLE_SHA1:-unknown}" "$STREAMER_COMMIT" "$BROKER_COMMIT" <<'PY'
import hashlib,json,pathlib,sys,zipfile
out=pathlib.Path(sys.argv[1]);pkg=out/'package';files={str(p.relative_to(pkg)).replace('\\','/'):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted(pkg.rglob('*')) if p.is_file()}
(pkg/'BUILD-PROVENANCE.json').write_text(json.dumps({'agent_commit':sys.argv[2],'source_streamer_commit':sys.argv[3],'source_broker_commit':sys.argv[4],'platform':'windows/amd64','files_sha256':files,'native_agent_source_lifecycle_tested':True,'encrypted_rtsp_tested':True,'source_broker_mtls_tested':True,'capture_tested':False,'media_tested':False,'hardware_tested':False,'audio_source':'synthesized silence','usb_os_attached':False,'production_parity':False,'provenance':'Independent reconstruction with licensed public dependencies, not recovered proprietary source.'},indent=2)+'\n')
archive=out/'USBridgeAgent-Windows-source-experimental.zip'
with zipfile.ZipFile(archive,'w',zipfile.ZIP_DEFLATED) as z:
    for p in sorted(pkg.rglob('*')):
        if p.is_file():z.write(p,p.relative_to(pkg).as_posix())
(out/'SHA256SUMS.txt').write_text(hashlib.sha256(archive.read_bytes()).hexdigest()+'  '+archive.name+'\n')
PY
