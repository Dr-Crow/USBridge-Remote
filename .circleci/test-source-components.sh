#!/usr/bin/env bash
set -euo pipefail
ROOT="$(pwd)"
OUT="$ROOT/artifacts/source-components"
WORK="$ROOT/.source-components-ci"
export PATH="$HOME/ci-toolchain/go/bin:$PATH" GOTOOLCHAIN=local CGO_ENABLED=1
sudo apt-get update
sudo env DEBIAN_FRONTEND=noninteractive NEEDRESTART_MODE=l apt-get install -y ffmpeg xvfb python3-cryptography libopus0
mkdir -p "$WORK" "$OUT/package/agent" "$OUT/package/components/bin" "$OUT/package/source"
/usr/bin/python3 - "$ROOT" "$WORK" <<'PY'
import hashlib,json,pathlib,sys,tarfile
root,work=map(pathlib.Path,sys.argv[1:]);snap=root/'.circleci/source-snapshots'
expected={'streamer':'2e07af3484369bc68bc8091d5a04969867eff0f9','broker':'176801ad075ad4d5e503d913f683948980baae2d'}
for name,pin in expected.items():
    meta=json.loads((snap/f'source-{name}.json').read_text());archive=snap/meta['archive']
    assert meta['commit']==pin
    assert hashlib.sha256(archive.read_bytes()).hexdigest()==meta['archive_sha256']
    target=work/name;target.mkdir(parents=True,exist_ok=True)
    with tarfile.open(archive) as t:
        members=t.getmembers()
        assert len(members)==meta['file_count']
        assert all(m.isfile() and m.name.startswith('source/') and '..' not in pathlib.PurePosixPath(m.name).parts for m in members)
        t.extractall(target)
PY
STREAMER_COMMIT=2e07af3484369bc68bc8091d5a04969867eff0f9
BROKER_COMMIT=176801ad075ad4d5e503d913f683948980baae2d
(cd "$WORK/streamer/source" && go test -race ./... && go build -trimpath -ldflags "-s -w -X main.version=$STREAMER_COMMIT" -o "$OUT/package/components/bin/source-streamer" ./cmd/source-streamer) 2>&1 | tee "$OUT/source-streamer-build-tests.txt"
(cd "$WORK/broker/source" && go test -race ./... && go build -trimpath -ldflags "-s -w -X main.version=$BROKER_COMMIT" -o "$OUT/package/components/bin/broker-session" ./cmd/broker-session) 2>&1 | tee "$OUT/source-broker-build-tests.txt"
cp "$ROOT/agent/dist/linux/usbridge-agent" "$OUT/package/agent/usbridge-agent"
bash "$WORK/streamer/source/controltransport/testdata/build_client.sh" "$WORK/enet-control-test-client"
cp "$ROOT/.circleci/source-snapshots/"* "$OUT/package/source/"
cp "$WORK/streamer/source/LICENSE" "$OUT/package/source/STREAMER-LICENSE"
cp "$WORK/broker/source/LICENSE" "$OUT/package/source/BROKER-LICENSE"
/usr/bin/python3 - "$OUT/package/components" "$STREAMER_COMMIT" "$BROKER_COMMIT" <<'PY'
import hashlib,json,pathlib,sys
root=pathlib.Path(sys.argv[1]);components=[]
for name,entry,version in [('source-streamer','bin/source-streamer',sys.argv[2]),('source-broker','bin/broker-session',sys.argv[3])]:
    data=(root/entry).read_bytes()
    components.append({'name':name,'platform':'linux/amd64','version':version,'profile':name+'-v1','entry':entry,'files':[{'path':entry,'size':len(data),'sha256':hashlib.sha256(data).hexdigest(),'executable':True}]})
(root/'manifest.json').write_text(json.dumps({'schema':1,'components':components},indent=2)+'\n')
(root/'MANIFEST.sha256').write_text(hashlib.sha256((root/'manifest.json').read_bytes()).hexdigest()+'  manifest.json\n')
PY
(cd "$WORK/broker/source" && AGENT_SOURCE_BINARY="$OUT/package/agent/usbridge-agent" BROKER_SESSION_BINARY="$OUT/package/components/bin/broker-session" "$HOME/ci-toolchain/go/bin/go" test -race -count=5 -run '^(TestPortableSessionExecutableMutualTLS|TestPortableAgentBrokerSessionMutualTLS)$' -v ./brokersession) 2>&1 | tee "$OUT/agent-broker-mutual-tls.txt"
/usr/bin/python3 "$ROOT/.circleci/source/test_agent_source_lifecycle.py" --agent "$OUT/package/agent/usbridge-agent" --streamer "$OUT/package/components/bin/source-streamer" --output "$OUT/lifecycle"
/usr/bin/python3 "$ROOT/.circleci/source/test_agent_source_media.py" --agent "$OUT/package/agent/usbridge-agent" --components "$OUT/package/components" --enet-helper "$WORK/enet-control-test-client" --output "$OUT/media"
cp "$ROOT/docs/SOURCE_COMPONENT_PACKAGE.md" "$OUT/package/README.md"
cp "$ROOT/docs/SOURCE_STREAMER_PROTOCOL.md" "$ROOT/docs/SOURCE_BROKER_PROTOCOL.md" "$OUT/package/"
/usr/bin/python3 - "$OUT" "${CIRCLE_SHA1:-unknown}" "$STREAMER_COMMIT" "$BROKER_COMMIT" <<'PY'
import hashlib,json,pathlib,sys
out=pathlib.Path(sys.argv[1]);files={str(p.relative_to(out/'package')):hashlib.sha256(p.read_bytes()).hexdigest() for p in sorted((out/'package').rglob('*')) if p.is_file()}
(out/'package/BUILD-PROVENANCE.json').write_text(json.dumps({'agent_commit':sys.argv[2],'source_streamer_commit':sys.argv[3],'source_broker_commit':sys.argv[4],'files_sha256':files,'hardware_tested':False,'audio_source':'synthesized silence','source_broker_included':True,'usb_os_attached':False,'production_parity':False,'provenance':'Independent reconstruction with licensed public dependencies, not recovered proprietary source.'},indent=2)+'\n')
PY
tar -czf "$OUT/USBridgeAgent-Linux-source-experimental.tar.gz" -C "$OUT/package" .
(cd "$OUT" && sha256sum USBridgeAgent-Linux-source-experimental.tar.gz > SHA256SUMS.txt)
